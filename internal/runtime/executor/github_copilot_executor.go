package executor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	copilotauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/copilot"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	log "github.com/sirupsen/logrus"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	githubCopilotBaseURL       = "https://api.business.githubcopilot.com"
	githubCopilotChatPath      = "/chat/completions"
	githubCopilotResponsesPath = "/responses"
	githubCopilotMessagesPath  = "/v1/messages"
	githubCopilotAuthType      = "github-copilot"
	githubCopilotTokenCacheTTL = 25 * time.Minute
	// tokenExpiryBuffer is the time before expiry when we should refresh the token.
	tokenExpiryBuffer = 5 * time.Minute
	// maxScannerBufferSize is the maximum buffer size for SSE scanning (20MB).
	maxScannerBufferSize = 20_971_520

	// Copilot API header values.
	copilotUserAgent     = "GitHubCopilotChat/0.38.2"
	copilotEditorVersion = "vscode/1.110.1"
	copilotPluginVersion = "copilot-chat/0.38.2"
	copilotIntegrationID = "vscode-chat"
	copilotOpenAIIntent  = "conversation-agent"
	copilotGitHubAPIVer  = "2025-10-01"
	copilotAnthropicBeta = "advanced-tool-use-2025-11-20,interleaved-thinking-2025-05-14"

	githubCopilotHeaderDiffTypeMissing        = "missing"
	githubCopilotHeaderDiffTypeExtra          = "extra"
	githubCopilotHeaderDiffTypeValueMismatch  = "value_mismatch"
	githubCopilotHeaderDiffTypeSourceMismatch = "source_mismatch"
)

// GitHubCopilotExecutor handles requests to the GitHub Copilot API.
type GitHubCopilotExecutor struct {
	cfg             *config.Config
	mu              sync.RWMutex
	cache           map[string]*cachedAPIToken
	initiatorBypass *initiatorBypassManager
	dualRunDiffSink func(githubCopilotHeaderDiffRecord)
}

// cachedAPIToken stores a cached Copilot API token with its expiry.
type cachedAPIToken struct {
	token       string
	apiEndpoint string
	expiresAt   time.Time
}

// NewGitHubCopilotExecutor constructs a new executor instance.
func NewGitHubCopilotExecutor(cfg *config.Config) *GitHubCopilotExecutor {
	var bypass *initiatorBypassManager
	if cfg != nil && cfg.GitHubCopilot.ForceAgentInitiatorBypass.Enabled {
		window := strings.TrimSpace(cfg.GitHubCopilot.ForceAgentInitiatorBypass.Window)
		stateFile := strings.TrimSpace(cfg.GitHubCopilot.ForceAgentInitiatorBypass.StateFile)
		if window == "" || stateFile == "" {
			log.Warn("github-copilot executor: force-agent-initiator-bypass enabled but window/state-file is empty; bypass disabled")
		} else if d, err := time.ParseDuration(window); err != nil || d <= 0 {
			log.Warnf("github-copilot executor: invalid force-agent-initiator-bypass window %q; bypass disabled", window)
		} else {
			bypass = newInitiatorBypassManager(d, stateFile, nil)
		}
	}
	return &GitHubCopilotExecutor{
		cfg:             cfg,
		cache:           make(map[string]*cachedAPIToken),
		initiatorBypass: bypass,
	}
}

// Identifier implements ProviderExecutor.
func (e *GitHubCopilotExecutor) Identifier() string { return githubCopilotAuthType }

// PrepareRequest implements ProviderExecutor.
func (e *GitHubCopilotExecutor) PrepareRequest(req *http.Request, auth *cliproxyauth.Auth) error {
	if req == nil {
		return nil
	}
	if isGitHubCopilotInternalAPIRequest(req) {
		if auth == nil {
			return statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
		}
		accessToken := metaStringValue(auth.Metadata, "access_token")
		if accessToken == "" {
			return statusErr{code: http.StatusUnauthorized, msg: "missing github access token"}
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		if strings.TrimSpace(req.Header.Get("Accept")) == "" {
			req.Header.Set("Accept", "application/json")
		}
		req.Header.Set("User-Agent", copilotUserAgent)
		return nil
	}
	ctx := req.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	apiToken, _, errToken := e.ensureAPIToken(ctx, auth)
	if errToken != nil {
		return errToken
	}
	useMessages := req.URL != nil && strings.HasPrefix(req.URL.Path, githubCopilotMessagesPath)
	e.applyHeaders(req, apiToken, auth, nil, false, useMessages, nil)
	return nil
}

func copilotContextHeaderValue(auth *cliproxyauth.Auth, key string) string {
	if auth == nil || auth.Metadata == nil {
		return ""
	}
	value, ok := auth.Metadata[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func applyGitHubCopilotContextHeaders(r *http.Request, auth *cliproxyauth.Auth) {
	if r == nil {
		return
	}
	mappings := []struct {
		metadataKey string
		headerKey   string
	}{
		{metadataKey: "editor_device_id", headerKey: "Editor-Device-Id"},
		{metadataKey: "vscode_abexpcontext", headerKey: "Vscode-Abexpcontext"},
		{metadataKey: "vscode_machineid", headerKey: "Vscode-Machineid"},
	}
	for _, mapping := range mappings {
		r.Header.Del(mapping.headerKey)
		if value := copilotContextHeaderValue(auth, mapping.metadataKey); value != "" {
			r.Header.Set(mapping.headerKey, value)
		}
	}
}

func isGitHubCopilotInternalAPIRequest(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return false
	}
	host := strings.TrimSpace(strings.ToLower(req.URL.Hostname()))
	if host != "api.github.com" {
		return false
	}
	path := strings.TrimSpace(req.URL.Path)
	return strings.HasPrefix(path, "/copilot_internal/")
}

// HttpRequest injects GitHub Copilot credentials into the request and executes it.
func (e *GitHubCopilotExecutor) HttpRequest(ctx context.Context, auth *cliproxyauth.Auth, req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, fmt.Errorf("github-copilot executor: request is nil")
	}
	if ctx == nil {
		ctx = req.Context()
	}
	httpReq := req.WithContext(ctx)
	if errPrepare := e.PrepareRequest(httpReq, auth); errPrepare != nil {
		return nil, errPrepare
	}
	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	return httpClient.Do(httpReq)
}

// Execute handles non-streaming requests to GitHub Copilot.
func (e *GitHubCopilotExecutor) Execute(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (resp cliproxyexecutor.Response, err error) {
	apiToken, baseURL, errToken := e.ensureAPIToken(ctx, auth)
	if errToken != nil {
		return resp, errToken
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	useMessages := useGitHubCopilotMessagesEndpoint(from, req.Model)
	useResponses := useGitHubCopilotResponsesEndpoint(from, req.Model)
	to := sdktranslator.FromString("openai")
	if useMessages {
		to = sdktranslator.FromString("claude")
	} else if useResponses {
		to = sdktranslator.FromString("openai-response")
	}
	originalPayload := bytes.Clone(req.Payload)
	if len(opts.OriginalRequest) > 0 {
		originalPayload = bytes.Clone(opts.OriginalRequest)
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, req.Model, originalPayload, false)
	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), false)
	body = e.normalizeModel(req.Model, body)
	if !useMessages {
		body = flattenAssistantContent(body)
	}

	// Detect vision content before input normalization removes messages
	hasVision := detectVisionContent(body)

	thinkingProvider := "openai"
	if useMessages {
		thinkingProvider = "claude"
	} else if useResponses {
		thinkingProvider = "codex"
	}
	body, err = applyThinkingWithUsageMeta(body, req.Model, from.String(), thinkingProvider, e.Identifier(), reporter)
	if err != nil {
		return resp, err
	}

	if useMessages {
		// Claude /v1/messages keeps original structure; no OpenAI tool/input normalization.
	} else if useResponses {
		body = normalizeGitHubCopilotResponsesInput(body)
		body = normalizeGitHubCopilotResponsesTools(body)
	} else {
		body = normalizeGitHubCopilotChatTools(body)
	}
	requestedModel := payloadRequestedModel(opts, req.Model)
	body = applyPayloadConfigWithRoot(e.cfg, req.Model, to.String(), "", body, originalTranslated, requestedModel)
	// For Claude /v1/messages: extract betas from body into header, and enforce thinking constraints.
	var extraBetas []string
	if useMessages {
		extraBetas, body = extractAndRemoveBetas(body)
		body = disableThinkingIfToolChoiceForced(body)
	}
	body, _ = sjson.SetBytes(body, "stream", false)

	// Inject a fake assistant message when force-agent-initiator is enabled and
	// the request body has no agent role (would otherwise produce X-Initiator: user).
	hasAgentRole := containsAgentConversationRole(body)
	if e.cfg.GitHubCopilot.ForceAgentInitiator && !hasAgentRole {
		bypassIdentity := e.initiatorBypassIdentity(auth, apiToken)
		if e.initiatorBypass == nil || !e.initiatorBypass.ShouldBypass(req.Model, bypassIdentity, false) {
			body = injectFakeAssistantMessage(body, e.cfg.GitHubCopilot.FakeAssistantContent, useResponses)
		}
	}

	path := selectGitHubCopilotEndpoint(from, req.Model)
	url := baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, err
	}
	e.applyHeaders(httpReq, apiToken, auth, body, false, useMessages, extraBetas)

	// Add Copilot-Vision-Request header if the request contains vision content
	if hasVision {
		httpReq.Header.Set("Copilot-Vision-Request", "true")
	}

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	defer func() {
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("github-copilot executor: close response body error: %v", errClose)
		}
	}()

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if !isHTTPSuccess(httpResp.StatusCode) {
		data, _ := io.ReadAll(httpResp.Body)
		appendAPIResponseChunk(ctx, e.cfg, data)
		log.Debugf("github-copilot executor: upstream error status: %d, body: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		err = statusErr{code: httpResp.StatusCode, msg: string(data)}
		return resp, err
	}

	data, err := io.ReadAll(httpResp.Body)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return resp, err
	}
	appendAPIResponseChunk(ctx, e.cfg, data)

	detail := parseOpenAIUsage(data)
	if useMessages {
		detail = parseClaudeUsage(data)
	} else if useResponses && detail.TotalTokens == 0 {
		detail = parseOpenAIResponsesUsage(data)
	}
	if detail.TotalTokens > 0 {
		reporter.publish(ctx, detail)
	}

	var param any
	converted := ""
	if useResponses && from.String() == "claude" {
		converted = translateGitHubCopilotResponsesNonStreamToClaude(data)
	} else {
		converted = sdktranslator.TranslateNonStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, data, &param)
	}
	resp = cliproxyexecutor.Response{Payload: []byte(converted)}
	reporter.ensurePublished(ctx)
	return resp, nil
}

// ExecuteStream handles streaming requests to GitHub Copilot.
func (e *GitHubCopilotExecutor) ExecuteStream(ctx context.Context, auth *cliproxyauth.Auth, req cliproxyexecutor.Request, opts cliproxyexecutor.Options) (_ *cliproxyexecutor.StreamResult, err error) {
	apiToken, baseURL, errToken := e.ensureAPIToken(ctx, auth)
	if errToken != nil {
		return nil, errToken
	}

	reporter := newUsageReporter(ctx, e.Identifier(), req.Model, auth)
	defer reporter.trackFailure(ctx, &err)

	from := opts.SourceFormat
	useMessages := useGitHubCopilotMessagesEndpoint(from, req.Model)
	useResponses := useGitHubCopilotResponsesEndpoint(from, req.Model)
	to := sdktranslator.FromString("openai")
	if useMessages {
		to = sdktranslator.FromString("claude")
	} else if useResponses {
		to = sdktranslator.FromString("openai-response")
	}
	originalPayload := bytes.Clone(req.Payload)
	if len(opts.OriginalRequest) > 0 {
		originalPayload = bytes.Clone(opts.OriginalRequest)
	}
	originalTranslated := sdktranslator.TranslateRequest(from, to, req.Model, originalPayload, false)
	body := sdktranslator.TranslateRequest(from, to, req.Model, bytes.Clone(req.Payload), true)
	body = e.normalizeModel(req.Model, body)
	if !useMessages {
		body = flattenAssistantContent(body)
	}

	// Detect vision content before input normalization removes messages
	hasVision := detectVisionContent(body)

	thinkingProvider := "openai"
	if useMessages {
		thinkingProvider = "claude"
	} else if useResponses {
		thinkingProvider = "codex"
	}
	body, err = applyThinkingWithUsageMeta(body, req.Model, from.String(), thinkingProvider, e.Identifier(), reporter)
	if err != nil {
		return nil, err
	}

	if useMessages {
		// Claude /v1/messages keeps original structure; no OpenAI tool/input normalization.
	} else if useResponses {
		body = normalizeGitHubCopilotResponsesInput(body)
		body = normalizeGitHubCopilotResponsesTools(body)
	} else {
		body = normalizeGitHubCopilotChatTools(body)
	}
	requestedModel := payloadRequestedModel(opts, req.Model)
	body = applyPayloadConfigWithRoot(e.cfg, req.Model, to.String(), "", body, originalTranslated, requestedModel)
	// For Claude /v1/messages: extract betas from body into header, and enforce thinking constraints.
	var extraBetas []string
	if useMessages {
		extraBetas, body = extractAndRemoveBetas(body)
		body = disableThinkingIfToolChoiceForced(body)
	}
	body, _ = sjson.SetBytes(body, "stream", true)

	// Inject a fake assistant message when force-agent-initiator is enabled and
	// the request body has no agent role (would otherwise produce X-Initiator: user).
	hasAgentRole := containsAgentConversationRole(body)
	if e.cfg.GitHubCopilot.ForceAgentInitiator && !hasAgentRole {
		bypassIdentity := e.initiatorBypassIdentity(auth, apiToken)
		if e.initiatorBypass == nil || !e.initiatorBypass.ShouldBypass(req.Model, bypassIdentity, false) {
			body = injectFakeAssistantMessage(body, e.cfg.GitHubCopilot.FakeAssistantContent, useResponses)
		}
	}

	path := selectGitHubCopilotEndpoint(from, req.Model)
	url := baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	e.applyHeaders(httpReq, apiToken, auth, body, true, useMessages, extraBetas)

	// Add Copilot-Vision-Request header if the request contains vision content
	if hasVision {
		httpReq.Header.Set("Copilot-Vision-Request", "true")
	}

	var authID, authLabel, authType, authValue string
	if auth != nil {
		authID = auth.ID
		authLabel = auth.Label
		authType, authValue = auth.AccountInfo()
	}
	recordAPIRequest(ctx, e.cfg, upstreamRequestLog{
		URL:       url,
		Method:    http.MethodPost,
		Headers:   httpReq.Header.Clone(),
		Body:      body,
		Provider:  e.Identifier(),
		AuthID:    authID,
		AuthLabel: authLabel,
		AuthType:  authType,
		AuthValue: authValue,
	})

	httpClient := newProxyAwareHTTPClient(ctx, e.cfg, auth, 0)
	httpResp, err := httpClient.Do(httpReq)
	if err != nil {
		recordAPIResponseError(ctx, e.cfg, err)
		return nil, err
	}

	recordAPIResponseMetadata(ctx, e.cfg, httpResp.StatusCode, httpResp.Header.Clone())

	if !isHTTPSuccess(httpResp.StatusCode) {
		data, readErr := io.ReadAll(httpResp.Body)
		if errClose := httpResp.Body.Close(); errClose != nil {
			log.Errorf("github-copilot executor: close response body error: %v", errClose)
		}
		if readErr != nil {
			recordAPIResponseError(ctx, e.cfg, readErr)
			return nil, readErr
		}
		appendAPIResponseChunk(ctx, e.cfg, data)
		log.Debugf("github-copilot executor: upstream error status: %d, body: %s", httpResp.StatusCode, summarizeErrorBody(httpResp.Header.Get("Content-Type"), data))
		err = statusErr{code: httpResp.StatusCode, msg: string(data)}
		return nil, err
	}

	out := make(chan cliproxyexecutor.StreamChunk)

	go func() {
		defer close(out)
		defer func() {
			if errClose := httpResp.Body.Close(); errClose != nil {
				log.Errorf("github-copilot executor: close response body error: %v", errClose)
			}
		}()

		scanner := bufio.NewScanner(httpResp.Body)
		scanner.Buffer(nil, maxScannerBufferSize)
		var param any

		// Track item IDs seen in SSE events to fix Copilot's mismatched IDs.
		// Copilot sometimes returns different encrypted IDs for the same logical
		// item across added/done/delta events. Some downstream SDKs use these IDs
		// as map keys, so mismatches crash the client.
		//
		// responsesOutputItemIDs: output_index → item.id from response.output_item.added
		// responsesContentPartIDs: "output_index:content_index" → canonical item_id for that content part
		// responsesReasoningSummaryPartIDs: "output_index:summary_index" → canonical item_id for that summary part
		responsesOutputItemIDs := map[int]string{}
		responsesContentPartIDs := map[string]string{}
		responsesReasoningSummaryPartIDs := map[string]string{}

		for scanner.Scan() {
			line := scanner.Bytes()
			appendAPIResponseChunk(ctx, e.cfg, line)

			// Parse SSE data
			if bytes.HasPrefix(line, dataTag) {
				data := bytes.TrimSpace(line[5:])
				if bytes.Equal(data, []byte("[DONE]")) {
					continue
				}
				if useMessages {
					if detail, ok := parseClaudeStreamUsage(line); ok {
						reporter.publish(ctx, detail)
					}
				} else if detail, ok := parseOpenAIStreamUsage(line); ok {
					reporter.publish(ctx, detail)
				} else if useResponses {
					if detail, ok := parseOpenAIResponsesStreamUsage(line); ok {
						reporter.publish(ctx, detail)
					}
				}
			}

			// When we are using the Copilot Claude /v1/messages endpoint and the downstream
			// client is also Claude, we should forward SSE lines directly. bufio.Scanner
			// drops the trailing '\n', so we add it back to preserve SSE framing.
			if useMessages && from == to {
				cloned := make([]byte, len(line)+1)
				copy(cloned, line)
				cloned[len(line)] = '\n'
				out <- cliproxyexecutor.StreamChunk{Payload: cloned}
				continue
			}

			// When we are using the Copilot OpenAI /responses endpoint and the downstream
			// client is also OpenAI Responses (openai-response -> openai-response), forward
			// SSE lines directly. The translator layer is line-based and will otherwise
			// drop SSE framing (event lines + blank delimiters), breaking clients.
			//
			// Additionally, fix mismatched reasoning item IDs: Copilot sometimes returns
			// different item.id values between output_item.added and output_item.done for
			// reasoning items. The ai-sdk uses item.id as a map key — mismatched IDs crash
			// the client with "TypeError: activeReasoningPart.summaryParts".
			if useResponses && from == to {
				patched := fixResponsesItemIDs(line, responsesOutputItemIDs, responsesContentPartIDs, responsesReasoningSummaryPartIDs)
				cloned := make([]byte, len(patched)+1)
				copy(cloned, patched)
				cloned[len(patched)] = '\n'
				out <- cliproxyexecutor.StreamChunk{Payload: cloned}
				continue
			}

			var chunks []string
			if useResponses && from.String() == "claude" {
				chunks = translateGitHubCopilotResponsesStreamToClaude(bytes.Clone(line), &param)
			} else {
				chunks = sdktranslator.TranslateStream(ctx, to, from, req.Model, bytes.Clone(opts.OriginalRequest), body, bytes.Clone(line), &param)
			}
			for i := range chunks {
				out <- cliproxyexecutor.StreamChunk{Payload: []byte(chunks[i])}
			}
		}

		if errScan := scanner.Err(); errScan != nil {
			recordAPIResponseError(ctx, e.cfg, errScan)
			reporter.publishFailure(ctx)
			out <- cliproxyexecutor.StreamChunk{Err: errScan}
		} else {
			reporter.ensurePublished(ctx)
		}
	}()

	return &cliproxyexecutor.StreamResult{
		Headers: httpResp.Header.Clone(),
		Chunks:  out,
	}, nil
}

// CountTokens is not supported for GitHub Copilot.
func (e *GitHubCopilotExecutor) CountTokens(_ context.Context, _ *cliproxyauth.Auth, _ cliproxyexecutor.Request, _ cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, statusErr{code: http.StatusNotImplemented, msg: "count tokens not supported for github-copilot"}
}

// Refresh validates the GitHub token is still working.
// GitHub OAuth tokens don't expire traditionally, so we just validate.
func (e *GitHubCopilotExecutor) Refresh(ctx context.Context, auth *cliproxyauth.Auth) (*cliproxyauth.Auth, error) {
	if auth == nil {
		return nil, statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
	}

	// Get the GitHub access token
	accessToken := metaStringValue(auth.Metadata, "access_token")
	if accessToken == "" {
		return auth, nil
	}

	// Validate the token can still get a Copilot API token
	copilotAuth := copilotauth.NewCopilotAuth(e.cfg)
	_, err := copilotAuth.GetCopilotAPIToken(ctx, accessToken)
	if err != nil {
		return nil, statusErr{code: http.StatusUnauthorized, msg: fmt.Sprintf("github-copilot token validation failed: %v", err)}
	}

	return auth, nil
}

// ensureAPIToken gets or refreshes the Copilot API token.
func (e *GitHubCopilotExecutor) ensureAPIToken(ctx context.Context, auth *cliproxyauth.Auth) (string, string, error) {
	if auth == nil {
		return "", "", statusErr{code: http.StatusUnauthorized, msg: "missing auth"}
	}

	// Get the GitHub access token
	accessToken := metaStringValue(auth.Metadata, "access_token")
	if accessToken == "" {
		return "", "", statusErr{code: http.StatusUnauthorized, msg: "missing github access token"}
	}

	// Check for cached API token using thread-safe access
	e.mu.RLock()
	if cached, ok := e.cache[accessToken]; ok && cached.expiresAt.After(time.Now().Add(tokenExpiryBuffer)) {
		e.mu.RUnlock()
		return cached.token, cached.apiEndpoint, nil
	}
	e.mu.RUnlock()

	// Get a new Copilot API token
	copilotAuth := copilotauth.NewCopilotAuth(e.cfg)
	apiToken, err := copilotAuth.GetCopilotAPIToken(ctx, accessToken)
	if err != nil {
		return "", "", statusErr{code: http.StatusUnauthorized, msg: fmt.Sprintf("failed to get copilot api token: %v", err)}
	}

	// Use endpoint from token response, fall back to default
	apiEndpoint := githubCopilotBaseURL
	if apiToken.Endpoints.API != "" {
		apiEndpoint = strings.TrimRight(apiToken.Endpoints.API, "/")
	}

	// Cache the token with thread-safe access
	expiresAt := time.Now().Add(githubCopilotTokenCacheTTL)
	if apiToken.ExpiresAt > 0 {
		expiresAt = time.Unix(apiToken.ExpiresAt, 0)
	}
	e.mu.Lock()
	e.cache[accessToken] = &cachedAPIToken{
		token:       apiToken.Token,
		apiEndpoint: apiEndpoint,
		expiresAt:   expiresAt,
	}
	e.mu.Unlock()

	return apiToken.Token, apiEndpoint, nil
}

func (e *GitHubCopilotExecutor) initiatorBypassIdentity(auth *cliproxyauth.Auth, apiToken string) string {
	if auth != nil {
		if id := strings.TrimSpace(auth.ID); id != "" {
			return "auth-id:" + id
		}
		if accessToken := strings.TrimSpace(metaStringValue(auth.Metadata, "access_token")); accessToken != "" {
			return "github-access-token:" + accessToken
		}
	}
	return "copilot-api-token:" + strings.TrimSpace(apiToken)
}

// applyHeaders sets the required headers for GitHub Copilot API requests.
func (e *GitHubCopilotExecutor) applyHeaders(r *http.Request, apiToken string, auth *cliproxyauth.Auth, body []byte, stream bool, useMessages bool, extraBetas []string) {
	var ginHeaders http.Header
	if ginCtx, ok := r.Context().Value("gin").(*gin.Context); ok && ginCtx != nil && ginCtx.Request != nil {
		ginHeaders = ginCtx.Request.Header
	}
	policy := e.githubCopilotHeaderPolicy()
	mode := strings.TrimSpace(strings.ToLower(policy.Mode))

	if mode == config.GitHubCopilotHeaderPolicyModeStrict {
		r.Header = make(http.Header)
	}

	setGitHubCopilotBaseHeaders(r.Header, apiToken, stream)

	switch mode {
	case config.GitHubCopilotHeaderPolicyModeStrict:
		compiled, _ := e.compileGitHubCopilotPolicyHeaders(ginHeaders, auth, body, apiToken, policy, policy.SessionStateFile, true, useMessages, extraBetas, false)
		applyHeadersFromMap(r.Header, compiled)
	case config.GitHubCopilotHeaderPolicyModeDualRun:
		e.applyGitHubCopilotLegacyHeaders(r, ginHeaders, auth, body, useMessages, extraBetas)
		legacy := r.Header.Clone()
		candidate, candidateAudit := e.compileGitHubCopilotPolicyHeaders(ginHeaders, auth, body, apiToken, policy, policy.ShadowStateFile, true, useMessages, extraBetas, false)
		e.emitGitHubCopilotDualRunDiffs(legacy, candidate, candidateAudit)
	default:
		e.applyGitHubCopilotLegacyHeaders(r, ginHeaders, auth, body, useMessages, extraBetas)
	}
}

type githubCopilotHeaderDiffSide struct {
	Source          string
	NormalizedValue string
	ValueHash       string
}

type githubCopilotHeaderDiffRecord struct {
	Header    string
	DiffType  string
	Legacy    githubCopilotHeaderDiffSide
	Candidate githubCopilotHeaderDiffSide
}

func (e *GitHubCopilotExecutor) githubCopilotHeaderPolicy() config.GitHubCopilotHeaderPolicyConfig {
	if e == nil || e.cfg == nil {
		return config.GitHubCopilotHeaderPolicyConfig{Mode: config.GitHubCopilotHeaderPolicyModeLegacy}
	}
	policy := sanitizeGitHubCopilotHeaderPolicy(e.cfg.GitHubCopilot.HeaderPolicy)
	mode := strings.TrimSpace(strings.ToLower(policy.Mode))
	switch mode {
	case config.GitHubCopilotHeaderPolicyModeDualRun, config.GitHubCopilotHeaderPolicyModeStrict:
		policy.Mode = mode
	default:
		policy.Mode = config.GitHubCopilotHeaderPolicyModeLegacy
	}
	return policy
}

func setGitHubCopilotBaseHeaders(headers http.Header, apiToken string, stream bool) {
	headers.Set("Content-Type", "application/json")
	headers.Set("Authorization", "Bearer "+apiToken)
	if stream {
		headers.Set("Accept", "text/event-stream")
	} else {
		headers.Set("Accept", "application/json")
	}
	headers.Set("Openai-Intent", copilotOpenAIIntent)
	headers.Set("Copilot-Integration-Id", copilotIntegrationID)
	headers.Set("X-Github-Api-Version", copilotGitHubAPIVer)
}

func (e *GitHubCopilotExecutor) applyGitHubCopilotLegacyHeaders(r *http.Request, incoming http.Header, auth *cliproxyauth.Auth, body []byte, useMessages bool, extraBetas []string) {
	r.Header.Set("User-Agent", copilotUserAgent)
	r.Header.Set("Editor-Version", copilotEditorVersion)
	r.Header.Set("Editor-Plugin-Version", copilotPluginVersion)
	r.Header.Set("X-Request-Id", generateGitHubCopilotLegacyRequestID())

	if useMessages {
		r.Header.Set("Anthropic-Beta", mergeGitHubCopilotBetaValues(copilotAnthropicBeta, incoming, extraBetas, true))
	} else {
		r.Header.Del("Anthropic-Beta")
	}

	forwardHeaders := []string{
		"Vscode-Sessionid",
		"X-Agent-Task-Id",
		"X-Interaction-Id",
		"X-Interaction-Type",
		"X-Vscode-User-Agent-Library-Version",
		"Sec-Fetch-Site",
		"Sec-Fetch-Mode",
		"Sec-Fetch-Dest",
		"Priority",
		"Accept-Encoding",
		"X-Request-Id",
	}
	if incoming != nil {
		for _, key := range forwardHeaders {
			if v := strings.TrimSpace(incoming.Get(key)); v != "" {
				r.Header.Set(key, v)
			}
		}
	}

	applyGitHubCopilotContextHeaders(r, auth)

	if requestID := strings.TrimSpace(r.Header.Get("X-Request-Id")); requestID != "" {
		r.Header.Set("X-Agent-Task-Id", requestID)
	} else {
		r.Header.Del("X-Agent-Task-Id")
	}

	r.Header.Set("X-Initiator", githubCopilotHeaderInitiator(body))
}

func generateGitHubCopilotLegacyRequestID() string {
	requestID, _ := generateGitHubCopilotRequestID()
	return requestID
}

func mergeGitHubCopilotBetaValues(base string, incoming http.Header, extraBetas []string, includeIncoming bool) string {
	betaSet := make(map[string]bool)
	ordered := make([]string, 0, 8)
	appendValue := func(raw string) {
		for _, token := range strings.Split(raw, ",") {
			trimmed := strings.TrimSpace(token)
			if trimmed == "" || betaSet[trimmed] {
				continue
			}
			betaSet[trimmed] = true
			ordered = append(ordered, trimmed)
		}
	}
	appendValue(base)
	if includeIncoming && incoming != nil {
		appendValue(incoming.Get("Anthropic-Beta"))
	}
	for _, beta := range extraBetas {
		appendValue(beta)
	}
	return strings.Join(ordered, ",")
}

func (e *GitHubCopilotExecutor) compileGitHubCopilotPolicyHeaders(incoming http.Header, auth *cliproxyauth.Auth, body []byte, apiToken string, policy config.GitHubCopilotHeaderPolicyConfig, stateFile string, strict bool, useMessages bool, extraBetas []string, includeIncomingBetas bool) (http.Header, githubCopilotHeaderCompileAudit) {
	compiler := newGitHubCopilotHeaderCompiler(strict, policy)
	if trimmedStateFile := strings.TrimSpace(stateFile); trimmedStateFile != "" {
		compiler.sessionStateManager = newGitHubCopilotSessionStateManager(trimmedStateFile)
	}
	compileContext := githubCopilotHeaderCompileContext{
		Body:           body,
		Model:          strings.TrimSpace(gjson.GetBytes(body, "model").String()),
		BucketIdentity: e.initiatorBypassIdentity(auth, apiToken),
	}
	headers, audit := compiler.CompileWithAudit(incoming, auth, compileContext)
	if useMessages {
		mergedBeta := mergeGitHubCopilotBetaValues(headers.Get("Anthropic-Beta"), incoming, extraBetas, includeIncomingBetas)
		if mergedBeta == "" {
			headers.Del("Anthropic-Beta")
		} else {
			headers.Set("Anthropic-Beta", mergedBeta)
		}
	} else {
		headers.Del("Anthropic-Beta")
	}
	return headers, audit
}

func applyHeadersFromMap(target http.Header, source http.Header) {
	for key := range source {
		values := source.Values(key)
		target.Del(key)
		for _, value := range values {
			target.Add(key, value)
		}
	}
}

func (e *GitHubCopilotExecutor) emitGitHubCopilotDualRunDiffs(legacy, candidate http.Header, candidateAudit githubCopilotHeaderCompileAudit) {
	for _, diff := range buildGitHubCopilotHeaderDiffs(legacy, candidate, candidateAudit) {
		if e != nil && e.dualRunDiffSink != nil {
			e.dualRunDiffSink(diff)
			continue
		}
		legacyNormalized := diff.Legacy.NormalizedValue
		candidateNormalized := diff.Candidate.NormalizedValue
		if isGitHubCopilotSensitiveHeader(diff.Header) {
			if legacyNormalized != "" {
				legacyNormalized = "[REDACTED]"
			}
			if candidateNormalized != "" {
				candidateNormalized = "[REDACTED]"
			}
		}
		log.WithFields(log.Fields{
			"header":                     diff.Header,
			"diff_type":                  diff.DiffType,
			"legacy.source":              diff.Legacy.Source,
			"legacy.normalized_value":    legacyNormalized,
			"legacy.value_hash":          diff.Legacy.ValueHash,
			"candidate.source":           diff.Candidate.Source,
			"candidate.normalized_value": candidateNormalized,
			"candidate.value_hash":       diff.Candidate.ValueHash,
		}).Info("github-copilot executor: dual-run header diff")
	}
}

func isGitHubCopilotSensitiveHeader(header string) bool {
	switch strings.ToLower(strings.TrimSpace(header)) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func buildGitHubCopilotHeaderDiffs(legacy, candidate http.Header, candidateAudit githubCopilotHeaderCompileAudit) []githubCopilotHeaderDiffRecord {
	if legacy == nil {
		legacy = http.Header{}
	}
	if candidate == nil {
		candidate = http.Header{}
	}
	candidateSources := map[string]string{}
	for key, source := range candidateAudit.HeaderSources {
		candidateSources[http.CanonicalHeaderKey(key)] = strings.TrimSpace(source)
	}

	keys := map[string]struct{}{}
	for key := range legacy {
		keys[http.CanonicalHeaderKey(key)] = struct{}{}
	}
	for key := range candidate {
		keys[http.CanonicalHeaderKey(key)] = struct{}{}
	}
	sortedKeys := make([]string, 0, len(keys))
	for key := range keys {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	result := make([]githubCopilotHeaderDiffRecord, 0, len(sortedKeys))
	for _, key := range sortedKeys {
		legacyValue := normalizeGitHubCopilotHeaderValue(legacy.Values(key))
		candidateValue := normalizeGitHubCopilotHeaderValue(candidate.Values(key))
		if legacyValue == "" && candidateValue == "" {
			continue
		}

		legacySource := githubCopilotHeaderSourceNone
		if legacyValue != "" {
			legacySource = githubCopilotHeaderSourceComputed
		}
		candidateSource := githubCopilotHeaderSourceNone
		if source := strings.TrimSpace(candidateSources[key]); source != "" {
			candidateSource = source
		} else if candidateValue != "" {
			candidateSource = githubCopilotHeaderSourceComputed
		}
		legacySource = normalizeGitHubCopilotHeaderSource(legacySource, legacyValue != "")
		candidateSource = normalizeGitHubCopilotHeaderSource(candidateSource, candidateValue != "")

		diffType := ""
		switch {
		case legacyValue != "" && candidateValue == "":
			diffType = githubCopilotHeaderDiffTypeMissing
		case legacyValue == "" && candidateValue != "":
			diffType = githubCopilotHeaderDiffTypeExtra
		case legacyValue != candidateValue:
			diffType = githubCopilotHeaderDiffTypeValueMismatch
		case legacySource != candidateSource:
			diffType = githubCopilotHeaderDiffTypeSourceMismatch
		default:
			continue
		}

		record := githubCopilotHeaderDiffRecord{
			Header:   key,
			DiffType: diffType,
			Legacy: githubCopilotHeaderDiffSide{
				Source:          legacySource,
				NormalizedValue: legacyValue,
				ValueHash:       hashGitHubCopilotHeaderValue(key, legacyValue),
			},
			Candidate: githubCopilotHeaderDiffSide{
				Source:          candidateSource,
				NormalizedValue: candidateValue,
				ValueHash:       hashGitHubCopilotHeaderValue(key, candidateValue),
			},
		}
		result = append(result, record)
	}
	return result
}

func normalizeGitHubCopilotHeaderSource(source string, hasValue bool) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case githubCopilotHeaderSourceComputed, "legacy-runtime", "legacy":
		return githubCopilotHeaderSourceComputed
	case githubCopilotHeaderSourceAuthMetadata, "auth-metadata":
		return githubCopilotHeaderSourceAuthMetadata
	case githubCopilotHeaderSourceConfig, "policy":
		return githubCopilotHeaderSourceConfig
	case githubCopilotHeaderSourceConstant, "default":
		return githubCopilotHeaderSourceConstant
	case githubCopilotHeaderSourceNone, "":
		if hasValue {
			return githubCopilotHeaderSourceComputed
		}
		return githubCopilotHeaderSourceConstant
	default:
		if hasValue {
			return githubCopilotHeaderSourceComputed
		}
		return githubCopilotHeaderSourceConstant
	}
}

func normalizeGitHubCopilotHeaderValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
		if trimmed != "" {
			normalized = append(normalized, trimmed)
		}
	}
	if len(normalized) == 0 {
		return ""
	}
	sort.Strings(normalized)
	return strings.Join(normalized, ",")
}

func hashGitHubCopilotHeaderValue(header, normalizedValue string) string {
	if strings.TrimSpace(normalizedValue) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(header)) + "\n" + normalizedValue))
	return hex.EncodeToString(sum[:])
}

func selectGitHubCopilotEndpoint(sourceFormat sdktranslator.Format, model string) string {
	if useGitHubCopilotMessagesEndpoint(sourceFormat, model) {
		return githubCopilotMessagesPath
	}
	if useGitHubCopilotResponsesEndpoint(sourceFormat, model) {
		return githubCopilotResponsesPath
	}
	return githubCopilotChatPath
}

func useGitHubCopilotMessagesEndpoint(_ sdktranslator.Format, model string) bool {
	baseModel := strings.ToLower(thinking.ParseSuffix(model).ModelName)
	return strings.Contains(baseModel, "claude")
}

func containsAgentConversationRole(body []byte) bool {
	if len(body) == 0 {
		return false
	}

	if messages := gjson.GetBytes(body, "messages"); messages.Exists() && messages.IsArray() {
		for _, item := range messages.Array() {
			role := strings.TrimSpace(strings.ToLower(item.Get("role").String()))
			if role == "assistant" || role == "tool" {
				return true
			}
		}
	}

	if inputs := gjson.GetBytes(body, "input"); inputs.Exists() && inputs.IsArray() {
		for _, item := range inputs.Array() {

			// Most Responses input items carry a top-level role.
			role := strings.TrimSpace(strings.ToLower(item.Get("role").String()))
			if role == "assistant" || role == "tool" {
				return true
			}

			switch strings.TrimSpace(strings.ToLower(item.Get("type").String())) {
			case "function_call", "function_call_arguments":
				return true
			case "function_call_output", "function_call_response", "tool_result":
				return true
			}
		}
	}

	return false
}

// fakeAssistantContentDefault is the default content for injected assistant messages.
const fakeAssistantContentDefault = "OK."

// injectFakeAssistantMessage inserts a fake assistant turn immediately before the
// last user-role message in the request body. If no user message is found, the
// assistant message is appended. Returns body unchanged when body is empty.
//
// Supported body shapes:
//   - messages[] — used by /chat/completions and /v1/messages
//   - input[]    — used by /responses (useResponses=true)
func injectFakeAssistantMessage(body []byte, content string, useResponses bool) []byte {
	if len(body) == 0 {
		return body
	}
	if strings.TrimSpace(content) == "" {
		content = fakeAssistantContentDefault
	}
	if useResponses {
		return injectFakeAssistantIntoInput(body, content)
	}
	return injectFakeAssistantIntoMessages(body, content)
}

// injectFakeAssistantIntoMessages handles the messages[] array format (OpenAI chat / Claude messages).
func injectFakeAssistantIntoMessages(body []byte, content string) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	items := messages.Array()

	// Find last user message index.
	lastUserIdx := -1
	for i, item := range items {
		if strings.ToLower(strings.TrimSpace(item.Get("role").String())) == "user" {
			lastUserIdx = i
		}
	}

	fakeMsg := map[string]any{
		"role":    "assistant",
		"content": content,
	}

	// Rebuild the array with the fake message inserted before the last user message.
	var newItems []any
	if lastUserIdx == -1 {
		// No user message: copy all items, append fake at end.
		for _, item := range items {
			var obj any
			if err := json.Unmarshal([]byte(item.Raw), &obj); err != nil {
				return body // malformed item: return original body unchanged
			}
			newItems = append(newItems, obj)
		}
		newItems = append(newItems, fakeMsg)
	} else {
		for i, item := range items {
			if i == lastUserIdx {
				newItems = append(newItems, fakeMsg)
			}
			var obj any
			if err := json.Unmarshal([]byte(item.Raw), &obj); err != nil {
				return body // malformed item: return original body unchanged
			}
			newItems = append(newItems, obj)
		}
	}

	newBody, err := sjson.SetBytes(body, "messages", newItems)
	if err != nil {
		return body
	}
	return newBody
}

// injectFakeAssistantIntoInput handles the input[] array format (Responses API).
func injectFakeAssistantIntoInput(body []byte, content string) []byte {
	inputs := gjson.GetBytes(body, "input")
	if !inputs.Exists() || !inputs.IsArray() {
		return body
	}
	items := inputs.Array()

	lastUserIdx := -1
	for i, item := range items {
		if strings.ToLower(strings.TrimSpace(item.Get("role").String())) == "user" {
			lastUserIdx = i
		}
	}

	fakeMsg := map[string]any{
		"type": "message",
		"role": "assistant",
		"content": []map[string]any{
			{"type": "output_text", "text": content},
		},
	}

	var newItems []any
	if lastUserIdx == -1 {
		for _, item := range items {
			var obj any
			if err := json.Unmarshal([]byte(item.Raw), &obj); err != nil {
				return body // malformed item: return original body unchanged
			}
			newItems = append(newItems, obj)
		}
		newItems = append(newItems, fakeMsg)
	} else {
		for i, item := range items {
			if i == lastUserIdx {
				newItems = append(newItems, fakeMsg)
			}
			var obj any
			if err := json.Unmarshal([]byte(item.Raw), &obj); err != nil {
				return body // malformed item: return original body unchanged
			}
			newItems = append(newItems, obj)
		}
	}

	newBody, err := sjson.SetBytes(body, "input", newItems)
	if err != nil {
		return body
	}
	return newBody
}

// detectVisionContent checks if the request body contains vision/image content.
// Returns true if the request includes image_url or image type content blocks.
func detectVisionContent(body []byte) bool {
	// Parse messages array
	messagesResult := gjson.GetBytes(body, "messages")
	if !messagesResult.Exists() || !messagesResult.IsArray() {
		return false
	}

	// Check each message for vision content
	for _, message := range messagesResult.Array() {
		content := message.Get("content")

		// If content is an array, check each content block
		if content.IsArray() {
			for _, block := range content.Array() {
				blockType := block.Get("type").String()
				// Check for image_url or image type
				if blockType == "image_url" || blockType == "image" {
					return true
				}
			}
		}
	}

	return false
}

// normalizeModel strips the suffix (e.g. "(medium)") from the model name
// before sending to GitHub Copilot, as the upstream API does not accept
// suffixed model identifiers.
func (e *GitHubCopilotExecutor) normalizeModel(model string, body []byte) []byte {
	baseModel := thinking.ParseSuffix(model).ModelName
	if strings.TrimSpace(baseModel) == "" {
		return body
	}
	// Ensure upstream request body always uses the executor-resolved model.
	// This covers both suffix stripping and OAuth model alias resolution, even
	// when the translator keeps the original payload's model field unchanged.
	body, _ = sjson.SetBytes(body, "model", baseModel)
	return body
}

func useGitHubCopilotResponsesEndpoint(sourceFormat sdktranslator.Format, model string) bool {
	if sourceFormat.String() == "openai-response" {
		return true
	}
	baseModel := strings.ToLower(thinking.ParseSuffix(model).ModelName)
	if strings.Contains(baseModel, "codex") {
		return true
	}
	for _, info := range registry.GetGitHubCopilotModels() {
		if info == nil || !strings.EqualFold(info.ID, baseModel) {
			continue
		}
		return len(info.SupportedEndpoints) == 1 && info.SupportedEndpoints[0] == githubCopilotResponsesPath
	}
	return false
}

// flattenAssistantContent converts assistant message content from array format
// to a joined string. GitHub Copilot requires assistant content as a string;
// sending it as an array causes Claude models to re-answer all previous prompts.
func flattenAssistantContent(body []byte) []byte {
	messages := gjson.GetBytes(body, "messages")
	if !messages.Exists() || !messages.IsArray() {
		return body
	}
	result := body
	for i, msg := range messages.Array() {
		if msg.Get("role").String() != "assistant" {
			continue
		}
		content := msg.Get("content")
		if !content.Exists() || !content.IsArray() {
			continue
		}
		// Skip flattening if the content contains non-text blocks (tool_use, thinking, etc.)
		hasNonText := false
		for _, part := range content.Array() {
			if t := part.Get("type").String(); t != "" && t != "text" {
				hasNonText = true
				break
			}
		}
		if hasNonText {
			continue
		}
		var textParts []string
		for _, part := range content.Array() {
			if part.Get("type").String() == "text" {
				if t := part.Get("text").String(); t != "" {
					textParts = append(textParts, t)
				}
			}
		}
		joined := strings.Join(textParts, "")
		path := fmt.Sprintf("messages.%d.content", i)
		result, _ = sjson.SetBytes(result, path, joined)
	}
	return result
}

func normalizeGitHubCopilotChatTools(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() {
		filtered := "[]"
		if tools.IsArray() {
			for _, tool := range tools.Array() {
				if tool.Get("type").String() != "function" {
					continue
				}
				filtered, _ = sjson.SetRaw(filtered, "-1", tool.Raw)
			}
		}
		body, _ = sjson.SetRawBytes(body, "tools", []byte(filtered))
	}

	toolChoice := gjson.GetBytes(body, "tool_choice")
	if !toolChoice.Exists() {
		return body
	}
	if toolChoice.Type == gjson.String {
		switch toolChoice.String() {
		case "auto", "none", "required":
			return body
		}
	}
	body, _ = sjson.SetBytes(body, "tool_choice", "auto")
	return body
}

func normalizeGitHubCopilotResponsesInput(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if input.Exists() {
		if input.Type == gjson.String {
			return body
		}
		if input.IsArray() {
			// Sanitize content types: some clients send "type":"text" inside
			// Responses API input, but Copilot requires "input_text"/"output_text".
			return sanitizeResponsesInputContentTypes(body)
		}
		// Non-string/non-array input: stringify as fallback.
		body, _ = sjson.SetBytes(body, "input", input.Raw)
		return body
	}

	// Convert Claude messages format to OpenAI Responses API input array.
	// This preserves the conversation structure (roles, tool calls, tool results)
	// which is critical for multi-turn tool-use conversations.
	inputArr := "[]"

	// System messages → developer role
	if system := gjson.GetBytes(body, "system"); system.Exists() {
		var systemParts []string
		if system.IsArray() {
			for _, part := range system.Array() {
				if txt := part.Get("text").String(); txt != "" {
					systemParts = append(systemParts, txt)
				}
			}
		} else if system.Type == gjson.String {
			systemParts = append(systemParts, system.String())
		}
		if len(systemParts) > 0 {
			msg := `{"type":"message","role":"developer","content":[]}`
			for _, txt := range systemParts {
				part := `{"type":"input_text","text":""}`
				part, _ = sjson.Set(part, "text", txt)
				msg, _ = sjson.SetRaw(msg, "content.-1", part)
			}
			inputArr, _ = sjson.SetRaw(inputArr, "-1", msg)
		}
	}

	// Messages → structured input items
	if messages := gjson.GetBytes(body, "messages"); messages.Exists() && messages.IsArray() {
		for _, msg := range messages.Array() {
			role := msg.Get("role").String()
			content := msg.Get("content")

			if !content.Exists() {
				continue
			}

			// Simple string content
			if content.Type == gjson.String {
				textType := "input_text"
				if role == "assistant" {
					textType = "output_text"
				}
				item := `{"type":"message","role":"","content":[]}`
				item, _ = sjson.Set(item, "role", role)
				part := fmt.Sprintf(`{"type":"%s","text":""}`, textType)
				part, _ = sjson.Set(part, "text", content.String())
				item, _ = sjson.SetRaw(item, "content.-1", part)
				inputArr, _ = sjson.SetRaw(inputArr, "-1", item)
				continue
			}

			if !content.IsArray() {
				continue
			}

			// Array content: split into message parts vs tool items
			var msgParts []string
			for _, c := range content.Array() {
				cType := c.Get("type").String()
				switch cType {
				case "text":
					textType := "input_text"
					if role == "assistant" {
						textType = "output_text"
					}
					part := fmt.Sprintf(`{"type":"%s","text":""}`, textType)
					part, _ = sjson.Set(part, "text", c.Get("text").String())
					msgParts = append(msgParts, part)
				case "image":
					source := c.Get("source")
					if source.Exists() {
						data := source.Get("data").String()
						if data == "" {
							data = source.Get("base64").String()
						}
						mediaType := source.Get("media_type").String()
						if mediaType == "" {
							mediaType = source.Get("mime_type").String()
						}
						if mediaType == "" {
							mediaType = "application/octet-stream"
						}
						if data != "" {
							part := `{"type":"input_image","image_url":""}`
							part, _ = sjson.Set(part, "image_url", fmt.Sprintf("data:%s;base64,%s", mediaType, data))
							msgParts = append(msgParts, part)
						}
					}
				case "tool_use":
					// Flush any accumulated message parts first
					if len(msgParts) > 0 {
						item := `{"type":"message","role":"","content":[]}`
						item, _ = sjson.Set(item, "role", role)
						for _, p := range msgParts {
							item, _ = sjson.SetRaw(item, "content.-1", p)
						}
						inputArr, _ = sjson.SetRaw(inputArr, "-1", item)
						msgParts = nil
					}
					fc := `{"type":"function_call","call_id":"","name":"","arguments":""}`
					fc, _ = sjson.Set(fc, "call_id", c.Get("id").String())
					fc, _ = sjson.Set(fc, "name", c.Get("name").String())
					if inputRaw := c.Get("input"); inputRaw.Exists() {
						fc, _ = sjson.Set(fc, "arguments", inputRaw.Raw)
					}
					inputArr, _ = sjson.SetRaw(inputArr, "-1", fc)
				case "tool_result":
					// Flush any accumulated message parts first
					if len(msgParts) > 0 {
						item := `{"type":"message","role":"","content":[]}`
						item, _ = sjson.Set(item, "role", role)
						for _, p := range msgParts {
							item, _ = sjson.SetRaw(item, "content.-1", p)
						}
						inputArr, _ = sjson.SetRaw(inputArr, "-1", item)
						msgParts = nil
					}
					fco := `{"type":"function_call_output","call_id":"","output":""}`
					fco, _ = sjson.Set(fco, "call_id", c.Get("tool_use_id").String())
					// Extract output text
					resultContent := c.Get("content")
					if resultContent.Type == gjson.String {
						fco, _ = sjson.Set(fco, "output", resultContent.String())
					} else if resultContent.IsArray() {
						var resultParts []string
						for _, rc := range resultContent.Array() {
							if txt := rc.Get("text").String(); txt != "" {
								resultParts = append(resultParts, txt)
							}
						}
						fco, _ = sjson.Set(fco, "output", strings.Join(resultParts, "\n"))
					} else if resultContent.Exists() {
						fco, _ = sjson.Set(fco, "output", resultContent.String())
					}
					inputArr, _ = sjson.SetRaw(inputArr, "-1", fco)
				case "thinking":
					// Skip thinking blocks - not part of the API input
				}
			}

			// Flush remaining message parts
			if len(msgParts) > 0 {
				item := `{"type":"message","role":"","content":[]}`
				item, _ = sjson.Set(item, "role", role)
				for _, p := range msgParts {
					item, _ = sjson.SetRaw(item, "content.-1", p)
				}
				inputArr, _ = sjson.SetRaw(inputArr, "-1", item)
			}
		}
	}

	body, _ = sjson.SetRawBytes(body, "input", []byte(inputArr))
	// Remove messages/system since we've converted them to input
	body, _ = sjson.DeleteBytes(body, "messages")
	body, _ = sjson.DeleteBytes(body, "system")
	return body
}

// sanitizeResponsesInputContentTypes rewrites "type":"text" content parts in a
// Responses API input[] array to the correct "input_text" or "output_text" based
// on the enclosing message role.  This fixes the 400 error from Copilot:
//
//	Invalid value: 'text'. Supported values are: 'input_text', 'input_image',
//	'output_text', 'refusal', 'input_file', 'computer_screenshot', 'summary_text'.
func sanitizeResponsesInputContentTypes(body []byte) []byte {
	input := gjson.GetBytes(body, "input")
	if !input.IsArray() {
		return body
	}
	dirty := false
	for i, item := range input.Array() {
		if item.Get("type").String() != "message" {
			continue
		}
		role := item.Get("role").String()
		content := item.Get("content")
		if !content.IsArray() {
			continue
		}
		for j, c := range content.Array() {
			if c.Get("type").String() != "text" {
				continue
			}
			newType := "input_text"
			if role == "assistant" {
				newType = "output_text"
			}
			path := fmt.Sprintf("input.%d.content.%d.type", i, j)
			body, _ = sjson.SetBytes(body, path, newType)
			dirty = true
		}
	}
	_ = dirty
	return body
}

func normalizeGitHubCopilotResponsesTools(body []byte) []byte {
	tools := gjson.GetBytes(body, "tools")
	if tools.Exists() {
		filtered := "[]"
		if tools.IsArray() {
			for _, tool := range tools.Array() {
				toolType := tool.Get("type").String()
				// Accept OpenAI format (type="function") and Claude format
				// (no type field, but has top-level name + input_schema).
				if toolType != "" && toolType != "function" {
					continue
				}
				name := tool.Get("name").String()
				if name == "" {
					name = tool.Get("function.name").String()
				}
				if name == "" {
					continue
				}
				normalized := `{"type":"function","name":""}`
				normalized, _ = sjson.Set(normalized, "name", name)
				if desc := tool.Get("description").String(); desc != "" {
					normalized, _ = sjson.Set(normalized, "description", desc)
				} else if desc = tool.Get("function.description").String(); desc != "" {
					normalized, _ = sjson.Set(normalized, "description", desc)
				}
				if params := tool.Get("parameters"); params.Exists() {
					normalized, _ = sjson.SetRaw(normalized, "parameters", params.Raw)
				} else if params = tool.Get("function.parameters"); params.Exists() {
					normalized, _ = sjson.SetRaw(normalized, "parameters", params.Raw)
				} else if params = tool.Get("input_schema"); params.Exists() {
					normalized, _ = sjson.SetRaw(normalized, "parameters", params.Raw)
				}
				filtered, _ = sjson.SetRaw(filtered, "-1", normalized)
			}
		}
		body, _ = sjson.SetRawBytes(body, "tools", []byte(filtered))
	}

	toolChoice := gjson.GetBytes(body, "tool_choice")
	if !toolChoice.Exists() {
		return body
	}
	if toolChoice.Type == gjson.String {
		switch toolChoice.String() {
		case "auto", "none", "required":
			return body
		default:
			body, _ = sjson.SetBytes(body, "tool_choice", "auto")
			return body
		}
	}
	if toolChoice.Type == gjson.JSON {
		choiceType := toolChoice.Get("type").String()
		if choiceType == "function" {
			name := toolChoice.Get("name").String()
			if name == "" {
				name = toolChoice.Get("function.name").String()
			}
			if name != "" {
				normalized := `{"type":"function","name":""}`
				normalized, _ = sjson.Set(normalized, "name", name)
				body, _ = sjson.SetRawBytes(body, "tool_choice", []byte(normalized))
				return body
			}
		}
	}
	body, _ = sjson.SetBytes(body, "tool_choice", "auto")
	return body
}

func collectTextFromNode(node gjson.Result) string {
	if !node.Exists() {
		return ""
	}
	if node.Type == gjson.String {
		return node.String()
	}
	if node.IsArray() {
		var parts []string
		for _, item := range node.Array() {
			if item.Type == gjson.String {
				if text := item.String(); text != "" {
					parts = append(parts, text)
				}
				continue
			}
			if text := item.Get("text").String(); text != "" {
				parts = append(parts, text)
				continue
			}
			if nested := collectTextFromNode(item.Get("content")); nested != "" {
				parts = append(parts, nested)
			}
		}
		return strings.Join(parts, "\n")
	}
	if node.Type == gjson.JSON {
		if text := node.Get("text").String(); text != "" {
			return text
		}
		if nested := collectTextFromNode(node.Get("content")); nested != "" {
			return nested
		}
		return node.Raw
	}
	return node.String()
}

type githubCopilotResponsesStreamToolState struct {
	Index int
	ID    string
	Name  string
}

type githubCopilotResponsesStreamState struct {
	MessageStarted    bool
	MessageStopSent   bool
	TextBlockStarted  bool
	TextBlockIndex    int
	NextContentIndex  int
	HasToolUse        bool
	ReasoningActive   bool
	ReasoningIndex    int
	OutputIndexToTool map[int]*githubCopilotResponsesStreamToolState
	ItemIDToTool      map[string]*githubCopilotResponsesStreamToolState
}

func translateGitHubCopilotResponsesNonStreamToClaude(data []byte) string {
	root := gjson.ParseBytes(data)
	out := `{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}`
	out, _ = sjson.Set(out, "id", root.Get("id").String())
	out, _ = sjson.Set(out, "model", root.Get("model").String())

	hasToolUse := false
	if output := root.Get("output"); output.Exists() && output.IsArray() {
		for _, item := range output.Array() {
			switch item.Get("type").String() {
			case "reasoning":
				var thinkingText string
				if summary := item.Get("summary"); summary.Exists() && summary.IsArray() {
					var parts []string
					for _, part := range summary.Array() {
						if txt := part.Get("text").String(); txt != "" {
							parts = append(parts, txt)
						}
					}
					thinkingText = strings.Join(parts, "")
				}
				if thinkingText == "" {
					if content := item.Get("content"); content.Exists() && content.IsArray() {
						var parts []string
						for _, part := range content.Array() {
							if txt := part.Get("text").String(); txt != "" {
								parts = append(parts, txt)
							}
						}
						thinkingText = strings.Join(parts, "")
					}
				}
				if thinkingText != "" {
					block := `{"type":"thinking","thinking":""}`
					block, _ = sjson.Set(block, "thinking", thinkingText)
					out, _ = sjson.SetRaw(out, "content.-1", block)
				}
			case "message":
				if content := item.Get("content"); content.Exists() && content.IsArray() {
					for _, part := range content.Array() {
						if part.Get("type").String() != "output_text" {
							continue
						}
						text := part.Get("text").String()
						if text == "" {
							continue
						}
						block := `{"type":"text","text":""}`
						block, _ = sjson.Set(block, "text", text)
						out, _ = sjson.SetRaw(out, "content.-1", block)
					}
				}
			case "function_call":
				hasToolUse = true
				toolUse := `{"type":"tool_use","id":"","name":"","input":{}}`
				toolID := item.Get("call_id").String()
				if toolID == "" {
					toolID = item.Get("id").String()
				}
				toolUse, _ = sjson.Set(toolUse, "id", toolID)
				toolUse, _ = sjson.Set(toolUse, "name", item.Get("name").String())
				if args := item.Get("arguments").String(); args != "" && gjson.Valid(args) {
					argObj := gjson.Parse(args)
					if argObj.IsObject() {
						toolUse, _ = sjson.SetRaw(toolUse, "input", argObj.Raw)
					}
				}
				out, _ = sjson.SetRaw(out, "content.-1", toolUse)
			}
		}
	}

	inputTokens := root.Get("usage.input_tokens").Int()
	outputTokens := root.Get("usage.output_tokens").Int()
	cachedTokens := root.Get("usage.input_tokens_details.cached_tokens").Int()
	if cachedTokens > 0 && inputTokens >= cachedTokens {
		inputTokens -= cachedTokens
	}
	out, _ = sjson.Set(out, "usage.input_tokens", inputTokens)
	out, _ = sjson.Set(out, "usage.output_tokens", outputTokens)
	if cachedTokens > 0 {
		out, _ = sjson.Set(out, "usage.cache_read_input_tokens", cachedTokens)
	}
	if hasToolUse {
		out, _ = sjson.Set(out, "stop_reason", "tool_use")
	} else if sr := root.Get("stop_reason").String(); sr == "max_tokens" || sr == "stop" {
		out, _ = sjson.Set(out, "stop_reason", sr)
	} else {
		out, _ = sjson.Set(out, "stop_reason", "end_turn")
	}
	return out
}

func translateGitHubCopilotResponsesStreamToClaude(line []byte, param *any) []string {
	if *param == nil {
		*param = &githubCopilotResponsesStreamState{
			TextBlockIndex:    -1,
			OutputIndexToTool: make(map[int]*githubCopilotResponsesStreamToolState),
			ItemIDToTool:      make(map[string]*githubCopilotResponsesStreamToolState),
		}
	}
	state := (*param).(*githubCopilotResponsesStreamState)

	if !bytes.HasPrefix(line, dataTag) {
		return nil
	}
	payload := bytes.TrimSpace(line[5:])
	if bytes.Equal(payload, []byte("[DONE]")) {
		return nil
	}
	if !gjson.ValidBytes(payload) {
		return nil
	}

	event := gjson.GetBytes(payload, "type").String()
	results := make([]string, 0, 4)
	ensureMessageStart := func() {
		if state.MessageStarted {
			return
		}
		messageStart := `{"type":"message_start","message":{"id":"","type":"message","role":"assistant","model":"","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":0,"output_tokens":0}}}`
		messageStart, _ = sjson.Set(messageStart, "message.id", gjson.GetBytes(payload, "response.id").String())
		messageStart, _ = sjson.Set(messageStart, "message.model", gjson.GetBytes(payload, "response.model").String())
		results = append(results, "event: message_start\ndata: "+messageStart+"\n\n")
		state.MessageStarted = true
	}
	startTextBlockIfNeeded := func() {
		if state.TextBlockStarted {
			return
		}
		if state.TextBlockIndex < 0 {
			state.TextBlockIndex = state.NextContentIndex
			state.NextContentIndex++
		}
		contentBlockStart := `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
		contentBlockStart, _ = sjson.Set(contentBlockStart, "index", state.TextBlockIndex)
		results = append(results, "event: content_block_start\ndata: "+contentBlockStart+"\n\n")
		state.TextBlockStarted = true
	}
	stopTextBlockIfNeeded := func() {
		if !state.TextBlockStarted {
			return
		}
		contentBlockStop := `{"type":"content_block_stop","index":0}`
		contentBlockStop, _ = sjson.Set(contentBlockStop, "index", state.TextBlockIndex)
		results = append(results, "event: content_block_stop\ndata: "+contentBlockStop+"\n\n")
		state.TextBlockStarted = false
		state.TextBlockIndex = -1
	}
	resolveTool := func(itemID string, outputIndex int) *githubCopilotResponsesStreamToolState {
		if itemID != "" {
			if tool, ok := state.ItemIDToTool[itemID]; ok {
				return tool
			}
		}
		if tool, ok := state.OutputIndexToTool[outputIndex]; ok {
			if itemID != "" {
				state.ItemIDToTool[itemID] = tool
			}
			return tool
		}
		return nil
	}

	switch event {
	case "response.created":
		ensureMessageStart()
	case "response.output_text.delta":
		ensureMessageStart()
		startTextBlockIfNeeded()
		delta := gjson.GetBytes(payload, "delta").String()
		if delta != "" {
			contentDelta := `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":""}}`
			contentDelta, _ = sjson.Set(contentDelta, "index", state.TextBlockIndex)
			contentDelta, _ = sjson.Set(contentDelta, "delta.text", delta)
			results = append(results, "event: content_block_delta\ndata: "+contentDelta+"\n\n")
		}
	case "response.reasoning_summary_part.added":
		ensureMessageStart()
		state.ReasoningActive = true
		state.ReasoningIndex = state.NextContentIndex
		state.NextContentIndex++
		thinkingStart := `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`
		thinkingStart, _ = sjson.Set(thinkingStart, "index", state.ReasoningIndex)
		results = append(results, "event: content_block_start\ndata: "+thinkingStart+"\n\n")
	case "response.reasoning_summary_text.delta":
		if state.ReasoningActive {
			delta := gjson.GetBytes(payload, "delta").String()
			if delta != "" {
				thinkingDelta := `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":""}}`
				thinkingDelta, _ = sjson.Set(thinkingDelta, "index", state.ReasoningIndex)
				thinkingDelta, _ = sjson.Set(thinkingDelta, "delta.thinking", delta)
				results = append(results, "event: content_block_delta\ndata: "+thinkingDelta+"\n\n")
			}
		}
	case "response.reasoning_summary_part.done":
		if state.ReasoningActive {
			thinkingStop := `{"type":"content_block_stop","index":0}`
			thinkingStop, _ = sjson.Set(thinkingStop, "index", state.ReasoningIndex)
			results = append(results, "event: content_block_stop\ndata: "+thinkingStop+"\n\n")
			state.ReasoningActive = false
		}
	case "response.output_item.added":
		if gjson.GetBytes(payload, "item.type").String() != "function_call" {
			break
		}
		ensureMessageStart()
		stopTextBlockIfNeeded()
		state.HasToolUse = true
		tool := &githubCopilotResponsesStreamToolState{
			Index: state.NextContentIndex,
			ID:    gjson.GetBytes(payload, "item.call_id").String(),
			Name:  gjson.GetBytes(payload, "item.name").String(),
		}
		if tool.ID == "" {
			tool.ID = gjson.GetBytes(payload, "item.id").String()
		}
		state.NextContentIndex++
		outputIndex := int(gjson.GetBytes(payload, "output_index").Int())
		state.OutputIndexToTool[outputIndex] = tool
		if itemID := gjson.GetBytes(payload, "item.id").String(); itemID != "" {
			state.ItemIDToTool[itemID] = tool
		}
		contentBlockStart := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"","name":"","input":{}}}`
		contentBlockStart, _ = sjson.Set(contentBlockStart, "index", tool.Index)
		contentBlockStart, _ = sjson.Set(contentBlockStart, "content_block.id", tool.ID)
		contentBlockStart, _ = sjson.Set(contentBlockStart, "content_block.name", tool.Name)
		results = append(results, "event: content_block_start\ndata: "+contentBlockStart+"\n\n")
	case "response.output_item.delta":
		item := gjson.GetBytes(payload, "item")
		if item.Get("type").String() != "function_call" {
			break
		}
		tool := resolveTool(item.Get("id").String(), int(gjson.GetBytes(payload, "output_index").Int()))
		if tool == nil {
			break
		}
		partial := gjson.GetBytes(payload, "delta").String()
		if partial == "" {
			partial = item.Get("arguments").String()
		}
		if partial == "" {
			break
		}
		inputDelta := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
		inputDelta, _ = sjson.Set(inputDelta, "index", tool.Index)
		inputDelta, _ = sjson.Set(inputDelta, "delta.partial_json", partial)
		results = append(results, "event: content_block_delta\ndata: "+inputDelta+"\n\n")
	case "response.function_call_arguments.delta":
		// Copilot sends tool call arguments via this event type (not response.output_item.delta).
		// Data format: {"delta":"...", "item_id":"...", "output_index":N, ...}
		itemID := gjson.GetBytes(payload, "item_id").String()
		outputIndex := int(gjson.GetBytes(payload, "output_index").Int())
		tool := resolveTool(itemID, outputIndex)
		if tool == nil {
			break
		}
		partial := gjson.GetBytes(payload, "delta").String()
		if partial == "" {
			break
		}
		inputDelta := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":""}}`
		inputDelta, _ = sjson.Set(inputDelta, "index", tool.Index)
		inputDelta, _ = sjson.Set(inputDelta, "delta.partial_json", partial)
		results = append(results, "event: content_block_delta\ndata: "+inputDelta+"\n\n")
	case "response.output_item.done":
		if gjson.GetBytes(payload, "item.type").String() != "function_call" {
			break
		}
		tool := resolveTool(gjson.GetBytes(payload, "item.id").String(), int(gjson.GetBytes(payload, "output_index").Int()))
		if tool == nil {
			break
		}
		contentBlockStop := `{"type":"content_block_stop","index":0}`
		contentBlockStop, _ = sjson.Set(contentBlockStop, "index", tool.Index)
		results = append(results, "event: content_block_stop\ndata: "+contentBlockStop+"\n\n")
	case "response.completed":
		ensureMessageStart()
		stopTextBlockIfNeeded()
		if !state.MessageStopSent {
			stopReason := "end_turn"
			if state.HasToolUse {
				stopReason = "tool_use"
			} else if sr := gjson.GetBytes(payload, "response.stop_reason").String(); sr == "max_tokens" || sr == "stop" {
				stopReason = sr
			}
			inputTokens := gjson.GetBytes(payload, "response.usage.input_tokens").Int()
			outputTokens := gjson.GetBytes(payload, "response.usage.output_tokens").Int()
			cachedTokens := gjson.GetBytes(payload, "response.usage.input_tokens_details.cached_tokens").Int()
			if cachedTokens > 0 && inputTokens >= cachedTokens {
				inputTokens -= cachedTokens
			}
			messageDelta := `{"type":"message_delta","delta":{"stop_reason":"","stop_sequence":null},"usage":{"input_tokens":0,"output_tokens":0}}`
			messageDelta, _ = sjson.Set(messageDelta, "delta.stop_reason", stopReason)
			messageDelta, _ = sjson.Set(messageDelta, "usage.input_tokens", inputTokens)
			messageDelta, _ = sjson.Set(messageDelta, "usage.output_tokens", outputTokens)
			if cachedTokens > 0 {
				messageDelta, _ = sjson.Set(messageDelta, "usage.cache_read_input_tokens", cachedTokens)
			}
			results = append(results, "event: message_delta\ndata: "+messageDelta+"\n\n")
			results = append(results, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			state.MessageStopSent = true
		}
	}

	return results
}

// isHTTPSuccess checks if the status code indicates success (2xx).
func isHTTPSuccess(statusCode int) bool {
	return statusCode >= 200 && statusCode < 300
}

const (
	// defaultCopilotContextLength is the default context window for unknown Copilot models.
	defaultCopilotContextLength = 128000
	// defaultCopilotMaxCompletionTokens is the default max output tokens for unknown Copilot models.
	defaultCopilotMaxCompletionTokens = 16384
)

// FetchGitHubCopilotModels dynamically fetches available models from the GitHub Copilot API.
// It exchanges the GitHub access token stored in auth.Metadata for a Copilot API token,
// then queries the /models endpoint. Falls back to the static registry on any failure.
func FetchGitHubCopilotModels(ctx context.Context, auth *cliproxyauth.Auth, cfg *config.Config) []*registry.ModelInfo {
	staticModels := registry.GetGitHubCopilotModels()
	if auth == nil {
		log.Debug("github-copilot: auth is nil, using static models")
		return staticModels
	}

	accessToken := metaStringValue(auth.Metadata, "access_token")
	if accessToken == "" {
		log.Debug("github-copilot: no access_token in auth metadata, using static models")
		return staticModels
	}

	copilotAuth := copilotauth.NewCopilotAuth(cfg)

	entries, err := copilotAuth.ListModelsWithGitHubToken(ctx, accessToken)
	if err != nil {
		log.Warnf("github-copilot: failed to fetch dynamic models: %v, using static models", err)
		return staticModels
	}

	if len(entries) == 0 {
		log.Debug("github-copilot: API returned no models, using static models")
		return staticModels
	}

	// Build a lookup from the static definitions so we can enrich dynamic entries
	// with known context lengths, thinking support, etc.
	staticMap := make(map[string]*registry.ModelInfo, len(staticModels))
	for _, m := range staticModels {
		if m == nil || m.ID == "" {
			continue
		}
		staticMap[m.ID] = m
	}

	now := time.Now().Unix()
	models := make([]*registry.ModelInfo, 0, len(entries)+len(staticModels))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			continue
		}
		// Deduplicate model IDs to avoid incorrect reference counting.
		if _, dup := seen[entry.ID]; dup {
			continue
		}
		seen[entry.ID] = struct{}{}

		m := &registry.ModelInfo{
			ID:      entry.ID,
			Object:  "model",
			Created: now,
			OwnedBy: "github-copilot",
			Type:    "github-copilot",
		}

		if entry.Created > 0 {
			m.Created = entry.Created
		}
		if entry.Name != "" {
			m.DisplayName = entry.Name
		} else {
			m.DisplayName = entry.ID
		}

		// Merge known metadata from the static fallback list
		if static, ok := staticMap[entry.ID]; ok {
			if m.DisplayName == entry.ID && static.DisplayName != "" {
				m.DisplayName = static.DisplayName
			}
			m.Description = static.Description
			m.ContextLength = static.ContextLength
			m.MaxCompletionTokens = static.MaxCompletionTokens
			m.SupportedEndpoints = static.SupportedEndpoints
			m.Thinking = static.Thinking
		} else {
			// Sensible defaults for models not in the static list
			m.Description = entry.ID + " via GitHub Copilot"
			m.ContextLength = defaultCopilotContextLength
			m.MaxCompletionTokens = defaultCopilotMaxCompletionTokens
		}

		models = append(models, m)
	}

	dynamicCount := len(models)
	var missingStaticIDs []string
	missingStatic := 0
	for _, m := range staticModels {
		if m == nil || m.ID == "" {
			continue
		}
		if _, ok := seen[m.ID]; ok {
			continue
		}
		models = append(models, m)
		missingStatic++
		if len(missingStaticIDs) < 10 {
			missingStaticIDs = append(missingStaticIDs, m.ID)
		}
	}
	if missingStatic > 0 {
		log.Warnf(
			"github-copilot: API model list missing %d static models; keeping static fallback (sample=%s)",
			missingStatic,
			strings.Join(missingStaticIDs, ","),
		)
	}

	log.Infof(
		"github-copilot: fetched %d models from API, merged with %d static models -> %d total",
		dynamicCount,
		len(staticModels),
		len(models),
	)
	return models
}

// fixResponsesItemIDs patches SSE data lines in the openai-response ->
// openai-response pass-through path to ensure IDs are consistent.
//
// Copilot sometimes returns different encrypted IDs for the same logical
// output item / content part across events. Some downstream SDKs use these
// IDs as map keys, so mismatches crash the client.
//
// Handled cases:
//  1. output items: item.id may differ between response.output_item.added,
//     response.output_item.done, and response.completed.
//  2. content parts: item_id may differ between response.content_part.added
//     and response.output_text.{delta,done} / response.content_part.done.
//     Additionally, Copilot may set content-part item_id to a different
//     value than the parent output item's id; clients often expect these
//     to match.
//
// Non-data lines and unrelated events are returned unchanged.
func fixResponsesItemIDs(line []byte, outputItemIDs map[int]string, contentPartIDs map[string]string, reasoningSummaryPartIDs map[string]string) []byte {
	if !bytes.HasPrefix(line, dataTag) {
		return line
	}
	data := bytes.TrimSpace(line[5:])
	if len(data) == 0 || data[0] != '{' {
		return line
	}

	eventType := gjson.GetBytes(data, "type").String()
	switch eventType {
	case "response.output_item.added":
		idx := int(gjson.GetBytes(data, "output_index").Int())
		id := gjson.GetBytes(data, "item.id").String()
		if id != "" {
			outputItemIDs[idx] = id
		}
		return line

	case "response.output_item.done":
		idx := int(gjson.GetBytes(data, "output_index").Int())
		addedID, ok := outputItemIDs[idx]
		if !ok {
			return line
		}
		currentID := gjson.GetBytes(data, "item.id").String()
		if currentID == addedID {
			return line
		}
		// Replace the mismatched ID
		fixed, err := sjson.SetBytes(data, "item.id", addedID)
		if err != nil {
			return line
		}
		return buildDataLine(fixed)

	case "response.content_part.added":
		// Record the canonical item_id for this content part.
		// Prefer the parent output item's ID when present (OpenAI semantics).
		outIdxInt := int(gjson.GetBytes(data, "output_index").Int())
		outIdx := gjson.GetBytes(data, "output_index").String()
		cntIdx := gjson.GetBytes(data, "content_index").String()
		key := outIdx + ":" + cntIdx

		currentItemID := gjson.GetBytes(data, "item_id").String()
		if currentItemID == "" {
			return line
		}
		canonicalItemID := currentItemID
		if parentID, ok := outputItemIDs[outIdxInt]; ok && parentID != "" {
			canonicalItemID = parentID
		}
		if canonicalItemID != "" {
			contentPartIDs[key] = canonicalItemID
		}
		if currentItemID == canonicalItemID {
			return line
		}
		fixed, err := sjson.SetBytes(data, "item_id", canonicalItemID)
		if err != nil {
			return line
		}
		return buildDataLine(fixed)

	case "response.reasoning_summary_part.added":
		// Record the canonical item_id for this reasoning summary part.
		outIdxInt := int(gjson.GetBytes(data, "output_index").Int())
		outIdx := gjson.GetBytes(data, "output_index").String()
		sumIdx := gjson.GetBytes(data, "summary_index").String()
		key := outIdx + ":" + sumIdx
		currentItemID := gjson.GetBytes(data, "item_id").String()
		if currentItemID == "" {
			return line
		}
		canonicalItemID := currentItemID
		if parentID, ok := outputItemIDs[outIdxInt]; ok && parentID != "" {
			canonicalItemID = parentID
		}
		if canonicalItemID != "" {
			reasoningSummaryPartIDs[key] = canonicalItemID
		}
		if currentItemID == canonicalItemID {
			return line
		}
		fixed, err := sjson.SetBytes(data, "item_id", canonicalItemID)
		if err != nil {
			return line
		}
		return buildDataLine(fixed)

	case "response.reasoning_summary_text.delta",
		"response.reasoning_summary_text.done",
		"response.reasoning_summary_part.done":
		outIdx := gjson.GetBytes(data, "output_index").String()
		sumIdx := gjson.GetBytes(data, "summary_index").String()
		key := outIdx + ":" + sumIdx
		addedItemID, ok := reasoningSummaryPartIDs[key]
		if !ok {
			// Fallback: if we know the parent output item id, normalise to it.
			outIdxInt := int(gjson.GetBytes(data, "output_index").Int())
			if parentID, ok2 := outputItemIDs[outIdxInt]; ok2 && parentID != "" {
				addedItemID = parentID
				reasoningSummaryPartIDs[key] = parentID
				ok = true
			}
		}
		if !ok || addedItemID == "" {
			return line
		}
		currentItemID := gjson.GetBytes(data, "item_id").String()
		if currentItemID == "" || currentItemID == addedItemID {
			return line
		}
		fixed, err := sjson.SetBytes(data, "item_id", addedItemID)
		if err != nil {
			return line
		}
		return buildDataLine(fixed)

	case "response.output_text.delta",
		"response.output_text.done",
		"response.content_part.done":
		outIdx := gjson.GetBytes(data, "output_index").String()
		cntIdx := gjson.GetBytes(data, "content_index").String()
		key := outIdx + ":" + cntIdx
		addedItemID, ok := contentPartIDs[key]
		if !ok {
			return line
		}
		currentItemID := gjson.GetBytes(data, "item_id").String()
		if currentItemID == "" {
			return line
		}
		if currentItemID == addedItemID {
			return line
		}
		fixed, err := sjson.SetBytes(data, "item_id", addedItemID)
		if err != nil {
			return line
		}
		return buildDataLine(fixed)

	case "response.completed":
		output := gjson.GetBytes(data, "response.output")
		if !output.Exists() || !output.IsArray() {
			return line
		}
		modified := false
		fixed := data
		for i, item := range output.Array() {
			currentID := item.Get("id").String()
			// The position in the output array corresponds to output_index.
			addedID, ok := outputItemIDs[i]
			if !ok || addedID == "" || currentID == addedID {
				continue
			}
			path := fmt.Sprintf("response.output.%d.id", i)
			var err error
			fixed, err = sjson.SetBytes(fixed, path, addedID)
			if err != nil {
				continue
			}
			modified = true
		}
		if !modified {
			return line
		}
		return buildDataLine(fixed)

	default:
		return line
	}
}

// buildDataLine constructs an SSE data line from JSON payload bytes.
func buildDataLine(jsonData []byte) []byte {
	result := make([]byte, 0, 6+len(jsonData))
	result = append(result, "data: "...)
	result = append(result, jsonData...)
	return result
}
