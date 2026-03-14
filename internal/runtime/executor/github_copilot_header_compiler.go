package executor

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type githubCopilotSessionStateResolver interface {
	ResolvePair(model, bucketIdentity, initiator string) (githubCopilotSessionPair, error)
}

type githubCopilotHeaderCompileContext struct {
	Body           []byte
	Model          string
	BucketIdentity string
}

const (
	githubCopilotHeaderSourceComputed     = "computed"
	githubCopilotHeaderSourceAuthMetadata = "auth_metadata"
	githubCopilotHeaderSourceConfig       = "config"
	githubCopilotHeaderSourceConstant     = "constant"
	githubCopilotHeaderSourceNone         = "none"

	githubCopilotSessionAuditDecisionDisabled = "disabled"
	githubCopilotSessionAuditDecisionResolved = "resolved"
	githubCopilotSessionAuditDecisionSkipped  = "skipped"
	githubCopilotSessionAuditDecisionError    = "error"

	githubCopilotSessionAuditReasonMissingModel  = "missing-model"
	githubCopilotSessionAuditReasonMissingBucket = "missing-bucket-identity"
	githubCopilotSessionAuditReasonResolveFailed = "resolve-pair-failed"

	githubCopilotRequestIDSourceV7       = "uuid-v7"
	githubCopilotRequestIDSourceRandom   = "uuid-random"
	githubCopilotRequestIDSourceFallback = "uuid-fallback-sha1"
)

var (
	githubCopilotRequestIDNewV7     = uuid.NewV7
	githubCopilotRequestIDNewRandom = uuid.NewRandom
	githubCopilotRequestIDFallback  = func() uuid.UUID {
		seed := fmt.Sprintf("%d|%s", time.Now().UTC().UnixNano(), copilotUserAgent)
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte(seed))
	}
)

type githubCopilotHeaderCompileAudit struct {
	RequestIDSource string
	HeaderSources   map[string]string
	Session         githubCopilotSessionCompileAudit
}

type githubCopilotSessionCompileAudit struct {
	Enabled        bool
	Decision       string
	Reason         string
	Model          string
	BucketIdentity string
	Initiator      string
}

type githubCopilotHeaderCompiler struct {
	strict              bool
	policy              config.GitHubCopilotHeaderPolicyConfig
	sessionStateManager githubCopilotSessionStateResolver
}

func newGitHubCopilotHeaderCompiler(strict bool, policy ...config.GitHubCopilotHeaderPolicyConfig) *githubCopilotHeaderCompiler {
	compiler := &githubCopilotHeaderCompiler{strict: strict}
	if len(policy) > 0 {
		compiler.policy = sanitizeGitHubCopilotHeaderPolicy(policy[0])
	}
	return compiler
}

func (c *githubCopilotHeaderCompiler) Compile(incoming http.Header, auth *cliproxyauth.Auth, context githubCopilotHeaderCompileContext) http.Header {
	headers, _ := c.CompileWithAudit(incoming, auth, context)
	return headers
}

func (c *githubCopilotHeaderCompiler) CompileWithAudit(incoming http.Header, auth *cliproxyauth.Auth, context githubCopilotHeaderCompileContext) (http.Header, githubCopilotHeaderCompileAudit) {
	headers := make(http.Header)
	if c != nil && !c.strict && incoming != nil {
		headers = incoming.Clone()
	}

	audit := githubCopilotHeaderCompileAudit{
		HeaderSources: map[string]string{},
		Session: githubCopilotSessionCompileAudit{
			Decision: githubCopilotSessionAuditDecisionDisabled,
		},
	}

	compileContext := context

	policy := config.GitHubCopilotHeaderPolicyConfig{}
	if c != nil {
		policy = c.policy
	}

	requestID, requestIDSource := generateGitHubCopilotRequestID()
	audit.RequestIDSource = requestIDSource
	headers.Set("X-Request-Id", requestID)
	headers.Set("X-Agent-Task-Id", requestID)

	headers.Set("X-Interaction-Type", "conversation-agent")
	headers.Set("X-Vscode-User-Agent-Library-Version", "electron-fetch")
	headers.Set("Sec-Fetch-Site", "none")
	headers.Set("Sec-Fetch-Mode", "no-cors")
	headers.Set("Sec-Fetch-Dest", "empty")
	headers.Set("Priority", "u=4, i")
	headers.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	resolvedUserAgent, userAgentSource := resolveHeaderValueWithSource(
		"",
		copilotMetadataHeaderValue(auth, "user_agent", "user-agent", "User-Agent"),
		policy.UserAgent,
		copilotUserAgent,
		"",
	)
	audit.HeaderSources["User-Agent"] = userAgentSource
	setGitHubCopilotResolvedHeader(headers, "User-Agent", resolvedUserAgent)

	resolvedEditorVersion, editorVersionSource := resolveHeaderValueWithSource(
		"",
		copilotMetadataHeaderValue(auth, "editor_version", "editor-version", "Editor-Version"),
		policy.EditorVersion,
		copilotEditorVersion,
		"",
	)
	audit.HeaderSources["Editor-Version"] = editorVersionSource
	setGitHubCopilotResolvedHeader(headers, "Editor-Version", resolvedEditorVersion)

	resolvedEditorPluginVersion, editorPluginVersionSource := resolveHeaderValueWithSource(
		"",
		copilotMetadataHeaderValue(auth, "editor_plugin_version", "editor-plugin-version", "Editor-Plugin-Version"),
		policy.EditorPluginVersion,
		copilotPluginVersion,
		"",
	)
	audit.HeaderSources["Editor-Plugin-Version"] = editorPluginVersionSource
	setGitHubCopilotResolvedHeader(headers, "Editor-Plugin-Version", resolvedEditorPluginVersion)

	resolvedAnthropicBeta, anthropicBetaSource := resolveHeaderValueWithSource(
		"",
		copilotMetadataHeaderValue(auth, "anthropic_beta", "anthropic-beta", "Anthropic-Beta"),
		policy.AnthropicBeta,
		copilotAnthropicBeta,
		"",
	)
	audit.HeaderSources["Anthropic-Beta"] = anthropicBetaSource
	setGitHubCopilotResolvedHeader(headers, "Anthropic-Beta", resolvedAnthropicBeta)

	setGitHubCopilotContextHeader(headers, auth, "editor_device_id", "Editor-Device-Id")
	setGitHubCopilotContextHeader(headers, auth, "vscode_abexpcontext", "Vscode-Abexpcontext")
	setGitHubCopilotContextHeader(headers, auth, "vscode_machineid", "Vscode-Machineid")

	initiator := githubCopilotHeaderInitiator(compileContext.Body)
	headers.Set("X-Initiator", initiator)

	pair := githubCopilotSessionPair{}
	if c != nil && c.sessionStateManager != nil {
		audit.Session.Enabled = true
		audit.Session.Initiator = initiator
		model := strings.TrimSpace(compileContext.Model)
		bucketIdentity := resolveGitHubCopilotSessionBucketIdentity(compileContext.BucketIdentity, auth)
		audit.Session.Model = model
		audit.Session.BucketIdentity = bucketIdentity

		switch {
		case model == "":
			audit.Session.Decision = githubCopilotSessionAuditDecisionSkipped
			audit.Session.Reason = githubCopilotSessionAuditReasonMissingModel
			log.Warn("github-copilot header compiler: skip session state resolve due to missing model")
		case bucketIdentity == "":
			audit.Session.Decision = githubCopilotSessionAuditDecisionSkipped
			audit.Session.Reason = githubCopilotSessionAuditReasonMissingBucket
			log.Warn("github-copilot header compiler: skip session state resolve due to missing bucket identity")
		default:
			resolvedPair, err := c.sessionStateManager.ResolvePair(model, bucketIdentity, initiator)
			if err != nil {
				audit.Session.Decision = githubCopilotSessionAuditDecisionError
				audit.Session.Reason = githubCopilotSessionAuditReasonResolveFailed
				log.Warnf("github-copilot header compiler: resolve session pair failed: %v", err)
			} else {
				audit.Session.Decision = githubCopilotSessionAuditDecisionResolved
			}
			pair = normalizeGitHubCopilotSessionPair(resolvedPair)
		}
	}
	setGitHubCopilotSessionPairHeaders(headers, pair)

	return headers, audit
}

func githubCopilotHeaderInitiator(body []byte) string {
	if containsAgentConversationRole(body) {
		return "agent"
	}
	return "user"
}

func resolveGitHubCopilotSessionBucketIdentity(explicit string, auth *cliproxyauth.Auth) string {
	if value := strings.TrimSpace(explicit); value != "" {
		return value
	}
	if auth != nil {
		if id := strings.TrimSpace(auth.ID); id != "" {
			return "auth-id:" + id
		}
		if accessToken := metaStringValue(auth.Metadata, "access_token"); accessToken != "" {
			return "github-access-token:" + accessToken
		}
	}
	return ""
}

func setGitHubCopilotSessionPairHeaders(headers http.Header, pair githubCopilotSessionPair) {
	headers.Del("Vscode-Sessionid")
	headers.Del("X-Interaction-Id")

	normalized := normalizeGitHubCopilotSessionPair(pair)
	if normalized == (githubCopilotSessionPair{}) {
		return
	}
	headers.Set("Vscode-Sessionid", normalized.SessionID)
	headers.Set("X-Interaction-Id", normalized.InteractionID)
}

func setGitHubCopilotContextHeader(headers http.Header, auth *cliproxyauth.Auth, metadataKey string, headerKey string) {
	headers.Del(headerKey)
	value := strings.TrimSpace(copilotContextHeaderValue(auth, metadataKey))
	if value != "" {
		headers.Set(headerKey, value)
	}
}

func setGitHubCopilotResolvedHeader(headers http.Header, headerKey, value string) {
	headers.Del(headerKey)
	if trimmed := strings.TrimSpace(value); trimmed != "" {
		headers.Set(headerKey, trimmed)
	}
}

func sanitizeGitHubCopilotHeaderPolicy(policy config.GitHubCopilotHeaderPolicyConfig) config.GitHubCopilotHeaderPolicyConfig {
	policy.UserAgent = strings.TrimSpace(policy.UserAgent)
	policy.EditorVersion = strings.TrimSpace(policy.EditorVersion)
	policy.EditorPluginVersion = strings.TrimSpace(policy.EditorPluginVersion)
	policy.AnthropicBeta = strings.TrimSpace(policy.AnthropicBeta)
	return policy
}

func copilotMetadataHeaderValue(auth *cliproxyauth.Auth, metadataKeys ...string) string {
	for _, key := range metadataKeys {
		if value := strings.TrimSpace(copilotContextHeaderValue(auth, key)); value != "" {
			return value
		}
	}
	return ""
}

func resolveHeaderValue(computed, authMetadata, configured, constantValue, incoming string) string {
	resolved, _ := resolveHeaderValueWithSource(computed, authMetadata, configured, constantValue, incoming)
	return resolved
}

func resolveHeaderValueWithSource(computed, authMetadata, configured, constantValue, incoming string) (string, string) {
	_ = incoming
	typedCandidates := []struct {
		value  string
		source string
	}{
		{value: computed, source: githubCopilotHeaderSourceComputed},
		{value: authMetadata, source: githubCopilotHeaderSourceAuthMetadata},
		{value: configured, source: githubCopilotHeaderSourceConfig},
		{value: constantValue, source: githubCopilotHeaderSourceConstant},
	}
	for _, candidate := range typedCandidates {
		if value := strings.TrimSpace(candidate.value); value != "" {
			return value, candidate.source
		}
	}
	return "", githubCopilotHeaderSourceNone
}

func generateGitHubCopilotRequestID() (string, string) {
	if requestID, err := githubCopilotRequestIDNewV7(); err == nil {
		return requestID.String(), githubCopilotRequestIDSourceV7
	}
	if requestID, err := githubCopilotRequestIDNewRandom(); err == nil {
		return requestID.String(), githubCopilotRequestIDSourceRandom
	}
	return githubCopilotRequestIDFallback().String(), githubCopilotRequestIDSourceFallback
}
