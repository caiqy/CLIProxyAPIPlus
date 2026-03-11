package executor

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
	"github.com/tidwall/gjson"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (f testRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestGitHubCopilotNormalizeModel_StripsSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		model     string
		wantModel string
	}{
		{
			name:      "suffix stripped",
			model:     "claude-opus-4.6(medium)",
			wantModel: "claude-opus-4.6",
		},
		{
			name:      "no suffix unchanged",
			model:     "claude-opus-4.6",
			wantModel: "claude-opus-4.6",
		},
		{
			name:      "different suffix stripped",
			model:     "gpt-4o(high)",
			wantModel: "gpt-4o",
		},
		{
			name:      "numeric suffix stripped",
			model:     "gemini-2.5-pro(8192)",
			wantModel: "gemini-2.5-pro",
		},
	}

	e := &GitHubCopilotExecutor{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body := []byte(`{"model":"` + tt.model + `","messages":[]}`)
			got := e.normalizeModel(tt.model, body)

			gotModel := gjson.GetBytes(got, "model").String()
			if gotModel != tt.wantModel {
				t.Fatalf("normalizeModel() model = %q, want %q", gotModel, tt.wantModel)
			}
		})
	}
}

func TestUseGitHubCopilotResponsesEndpoint_OpenAIResponseSource(t *testing.T) {
	t.Parallel()
	if !useGitHubCopilotResponsesEndpoint(sdktranslator.FromString("openai-response"), "claude-3-5-sonnet") {
		t.Fatal("expected openai-response source to use /responses")
	}
}

func TestGitHubCopilotPrepareRequest_UsesBearerAccessTokenForCopilotInternalAPI(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	e := NewGitHubCopilotExecutor(cfg)
	accessToken := "gho_request_token"
	e.cache[accessToken] = &cachedAPIToken{
		token:       "api_token_from_exchange",
		apiEndpoint: githubCopilotBaseURL,
		expiresAt:   time.Now().Add(time.Hour),
	}

	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": accessToken}}
	req, errReq := http.NewRequest(http.MethodGet, "https://api.github.com/copilot_internal/user", nil)
	if errReq != nil {
		t.Fatalf("new request: %v", errReq)
	}

	err := e.PrepareRequest(req, auth)
	if err != nil {
		t.Fatalf("PrepareRequest returned error: %v", err)
	}

	if got := req.Header.Get("Authorization"); got != "Bearer "+accessToken {
		t.Fatalf("authorization = %q, want %q", got, "Bearer "+accessToken)
	}
}

func TestUseGitHubCopilotResponsesEndpoint_CodexModel(t *testing.T) {
	t.Parallel()
	if !useGitHubCopilotResponsesEndpoint(sdktranslator.FromString("openai"), "gpt-5-codex") {
		t.Fatal("expected codex model to use /responses")
	}
}

func TestUseGitHubCopilotResponsesEndpoint_ResponsesOnlyStaticModel(t *testing.T) {
	t.Parallel()
	if !useGitHubCopilotResponsesEndpoint(sdktranslator.FromString("openai"), "gpt-5.4") {
		t.Fatal("expected responses-only static model to use /responses")
	}
}

func TestUseGitHubCopilotResponsesEndpoint_DefaultChat(t *testing.T) {
	t.Parallel()
	if useGitHubCopilotResponsesEndpoint(sdktranslator.FromString("openai"), "claude-3-5-sonnet") {
		t.Fatal("expected default openai source with non-codex model to use /chat/completions")
	}
}

func TestUseGitHubCopilotMessagesEndpoint_ClaudeModel(t *testing.T) {
	t.Parallel()
	if !useGitHubCopilotMessagesEndpoint(sdktranslator.FromString("openai"), "claude-sonnet-4.6") {
		t.Fatal("expected claude model to use /v1/messages")
	}
}

func TestSelectGitHubCopilotEndpoint_ClaudeTakesPriorityOverResponses(t *testing.T) {
	t.Parallel()
	path := selectGitHubCopilotEndpoint(sdktranslator.FromString("openai-response"), "claude-sonnet-4.6")
	if path != githubCopilotMessagesPath {
		t.Fatalf("endpoint = %q, want %q", path, githubCopilotMessagesPath)
	}
}

func TestNormalizeGitHubCopilotChatTools_KeepFunctionOnly(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"type":"function","function":{"name":"ok"}},{"type":"code_interpreter"}],"tool_choice":"auto"}`)
	got := normalizeGitHubCopilotChatTools(body)
	tools := gjson.GetBytes(got, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Get("type").String() != "function" {
		t.Fatalf("tool type = %q, want function", tools[0].Get("type").String())
	}
}

func TestNormalizeGitHubCopilotChatTools_InvalidToolChoiceDowngradeToAuto(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[],"tool_choice":{"type":"function","function":{"name":"x"}}}`)
	got := normalizeGitHubCopilotChatTools(body)
	if gjson.GetBytes(got, "tool_choice").String() != "auto" {
		t.Fatalf("tool_choice = %s, want auto", gjson.GetBytes(got, "tool_choice").Raw)
	}
}

func TestNormalizeGitHubCopilotResponsesInput_MissingInputExtractedFromSystemAndMessages(t *testing.T) {
	t.Parallel()
	body := []byte(`{"system":"sys text","messages":[{"role":"user","content":"user text"},{"role":"assistant","content":[{"type":"text","text":"assistant text"}]}]}`)
	got := normalizeGitHubCopilotResponsesInput(body)
	in := gjson.GetBytes(got, "input")
	if !in.IsArray() {
		t.Fatalf("input type = %v, want array", in.Type)
	}
	raw := in.Raw
	if !strings.Contains(raw, "sys text") || !strings.Contains(raw, "user text") || !strings.Contains(raw, "assistant text") {
		t.Fatalf("input = %s, want structured array with all texts", raw)
	}
	if gjson.GetBytes(got, "messages").Exists() {
		t.Fatal("messages should be removed after conversion")
	}
	if gjson.GetBytes(got, "system").Exists() {
		t.Fatal("system should be removed after conversion")
	}
}

func TestNormalizeGitHubCopilotResponsesInput_NonStringInputStringified(t *testing.T) {
	t.Parallel()
	body := []byte(`{"input":{"foo":"bar"}}`)
	got := normalizeGitHubCopilotResponsesInput(body)
	in := gjson.GetBytes(got, "input")
	if in.Type != gjson.String {
		t.Fatalf("input type = %v, want string", in.Type)
	}
	if !strings.Contains(in.String(), "foo") {
		t.Fatalf("input = %q, want stringified object", in.String())
	}
}

func TestNormalizeGitHubCopilotResponsesInput_SanitizesTextContentType(t *testing.T) {
	t.Parallel()
	// Simulates a client that sends openai-response format with "type":"text" instead of
	// "type":"input_text"/"output_text". The normalize function should fix these on pass-through
	// to avoid Copilot rejecting the request with:
	//   Invalid value: 'text'. Supported values are: 'input_text', 'input_image', 'output_text', ...
	body := []byte(`{
		"model":"gpt-5.4",
		"input":[
			{"type":"message","role":"user","content":[{"type":"text","text":"hello"}]},
			{"type":"message","role":"assistant","content":[{"type":"text","text":"hi there"}]},
			{"type":"message","role":"developer","content":[{"type":"text","text":"system prompt"}]}
		]
	}`)
	got := normalizeGitHubCopilotResponsesInput(body)
	input := gjson.GetBytes(got, "input")
	if !input.IsArray() {
		t.Fatalf("input should be an array, got %v", input.Type)
	}
	for _, item := range input.Array() {
		role := item.Get("role").String()
		for _, c := range item.Get("content").Array() {
			cType := c.Get("type").String()
			if cType == "text" {
				t.Fatalf("content type should not be 'text' after normalization, role=%s, got: %s", role, item.Raw)
			}
			switch role {
			case "user", "developer":
				if cType != "input_text" {
					t.Fatalf("expected input_text for role=%s, got %q", role, cType)
				}
			case "assistant":
				if cType != "output_text" {
					t.Fatalf("expected output_text for role=assistant, got %q", cType)
				}
			}
		}
	}
}

func TestNormalizeGitHubCopilotResponsesInput_PreservesValidInputTypes(t *testing.T) {
	t.Parallel()
	// Input that already uses correct types should NOT be modified.
	body := []byte(`{
		"model":"gpt-5.4",
		"input":[
			{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]},
			{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hi"}]},
			{"type":"function_call","call_id":"c1","name":"bash","arguments":"{}"},
			{"type":"function_call_output","call_id":"c1","output":"ok"}
		]
	}`)
	got := normalizeGitHubCopilotResponsesInput(body)
	input := gjson.GetBytes(got, "input")
	// Verify the first message content type is still input_text
	firstContent := input.Array()[0].Get("content.0.type").String()
	if firstContent != "input_text" {
		t.Fatalf("expected input_text preserved, got %q", firstContent)
	}
	// Verify function_call items are untouched
	if input.Array()[2].Get("type").String() != "function_call" {
		t.Fatalf("expected function_call item preserved")
	}
}

func TestNormalizeGitHubCopilotResponsesTools_FlattenFunctionTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"type":"function","function":{"name":"sum","description":"d","parameters":{"type":"object"}}},{"type":"web_search"}]}`)
	got := normalizeGitHubCopilotResponsesTools(body)
	tools := gjson.GetBytes(got, "tools").Array()
	if len(tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(tools))
	}
	if tools[0].Get("name").String() != "sum" {
		t.Fatalf("tools[0].name = %q, want sum", tools[0].Get("name").String())
	}
	if !tools[0].Get("parameters").Exists() {
		t.Fatal("expected parameters to be preserved")
	}
}

func TestNormalizeGitHubCopilotResponsesTools_ClaudeFormatTools(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tools":[{"name":"Bash","description":"Run commands","input_schema":{"type":"object","properties":{"command":{"type":"string"}},"required":["command"]}},{"name":"Read","description":"Read files","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}]}`)
	got := normalizeGitHubCopilotResponsesTools(body)
	tools := gjson.GetBytes(got, "tools").Array()
	if len(tools) != 2 {
		t.Fatalf("tools len = %d, want 2", len(tools))
	}
	if tools[0].Get("type").String() != "function" {
		t.Fatalf("tools[0].type = %q, want function", tools[0].Get("type").String())
	}
	if tools[0].Get("name").String() != "Bash" {
		t.Fatalf("tools[0].name = %q, want Bash", tools[0].Get("name").String())
	}
	if tools[0].Get("description").String() != "Run commands" {
		t.Fatalf("tools[0].description = %q, want 'Run commands'", tools[0].Get("description").String())
	}
	if !tools[0].Get("parameters").Exists() {
		t.Fatal("expected parameters to be set from input_schema")
	}
	if tools[0].Get("parameters.properties.command").Exists() != true {
		t.Fatal("expected parameters.properties.command to exist")
	}
	if tools[1].Get("name").String() != "Read" {
		t.Fatalf("tools[1].name = %q, want Read", tools[1].Get("name").String())
	}
}

func TestNormalizeGitHubCopilotResponsesTools_FlattenToolChoiceFunctionObject(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tool_choice":{"type":"function","function":{"name":"sum"}}}`)
	got := normalizeGitHubCopilotResponsesTools(body)
	if gjson.GetBytes(got, "tool_choice.type").String() != "function" {
		t.Fatalf("tool_choice.type = %q, want function", gjson.GetBytes(got, "tool_choice.type").String())
	}
	if gjson.GetBytes(got, "tool_choice.name").String() != "sum" {
		t.Fatalf("tool_choice.name = %q, want sum", gjson.GetBytes(got, "tool_choice.name").String())
	}
}

func TestNormalizeGitHubCopilotResponsesTools_InvalidToolChoiceDowngradeToAuto(t *testing.T) {
	t.Parallel()
	body := []byte(`{"tool_choice":{"type":"function"}}`)
	got := normalizeGitHubCopilotResponsesTools(body)
	if gjson.GetBytes(got, "tool_choice").String() != "auto" {
		t.Fatalf("tool_choice = %s, want auto", gjson.GetBytes(got, "tool_choice").Raw)
	}
}

func TestTranslateGitHubCopilotResponsesNonStreamToClaude_TextMapping(t *testing.T) {
	t.Parallel()
	resp := []byte(`{"id":"resp_1","model":"gpt-5-codex","output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}],"usage":{"input_tokens":3,"output_tokens":5}}`)
	out := translateGitHubCopilotResponsesNonStreamToClaude(resp)
	if gjson.Get(out, "type").String() != "message" {
		t.Fatalf("type = %q, want message", gjson.Get(out, "type").String())
	}
	if gjson.Get(out, "content.0.type").String() != "text" {
		t.Fatalf("content.0.type = %q, want text", gjson.Get(out, "content.0.type").String())
	}
	if gjson.Get(out, "content.0.text").String() != "hello" {
		t.Fatalf("content.0.text = %q, want hello", gjson.Get(out, "content.0.text").String())
	}
}

func TestTranslateGitHubCopilotResponsesNonStreamToClaude_ToolUseMapping(t *testing.T) {
	t.Parallel()
	resp := []byte(`{"id":"resp_2","model":"gpt-5-codex","output":[{"type":"function_call","id":"fc_1","call_id":"call_1","name":"sum","arguments":"{\"a\":1}"}],"usage":{"input_tokens":1,"output_tokens":2}}`)
	out := translateGitHubCopilotResponsesNonStreamToClaude(resp)
	if gjson.Get(out, "content.0.type").String() != "tool_use" {
		t.Fatalf("content.0.type = %q, want tool_use", gjson.Get(out, "content.0.type").String())
	}
	if gjson.Get(out, "content.0.name").String() != "sum" {
		t.Fatalf("content.0.name = %q, want sum", gjson.Get(out, "content.0.name").String())
	}
	if gjson.Get(out, "stop_reason").String() != "tool_use" {
		t.Fatalf("stop_reason = %q, want tool_use", gjson.Get(out, "stop_reason").String())
	}
}

func TestTranslateGitHubCopilotResponsesStreamToClaude_TextLifecycle(t *testing.T) {
	t.Parallel()
	var param any

	created := translateGitHubCopilotResponsesStreamToClaude([]byte(`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5-codex"}}`), &param)
	if len(created) == 0 || !strings.Contains(created[0], "message_start") {
		t.Fatalf("created events = %#v, want message_start", created)
	}

	delta := translateGitHubCopilotResponsesStreamToClaude([]byte(`data: {"type":"response.output_text.delta","delta":"he"}`), &param)
	joinedDelta := strings.Join(delta, "")
	if !strings.Contains(joinedDelta, "content_block_start") || !strings.Contains(joinedDelta, "text_delta") {
		t.Fatalf("delta events = %#v, want content_block_start + text_delta", delta)
	}

	completed := translateGitHubCopilotResponsesStreamToClaude([]byte(`data: {"type":"response.completed","response":{"usage":{"input_tokens":7,"output_tokens":9}}}`), &param)
	joinedCompleted := strings.Join(completed, "")
	if !strings.Contains(joinedCompleted, "message_delta") || !strings.Contains(joinedCompleted, "message_stop") {
		t.Fatalf("completed events = %#v, want message_delta + message_stop", completed)
	}
}

// --- Tests for X-Initiator detection logic (Problem L) ---

func TestApplyHeaders_XInitiator_UserOnly(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	body := []byte(`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "user" {
		t.Fatalf("X-Initiator = %q, want user", got)
	}
}

func TestApplyHeaders_XInitiator_AgentWhenAnyAssistantRoleExists(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	// Align with copilot-api: any assistant/tool role makes initiator agent.
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":"I will read the file"},{"role":"user","content":"tool result here"}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "agent" {
		t.Fatalf("X-Initiator = %q, want agent (assistant role exists)", got)
	}
}

func TestApplyHeaders_XInitiator_AgentWithToolRole(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	body := []byte(`{"messages":[{"role":"user","content":"hello"},{"role":"tool","content":"result"}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "agent" {
		t.Fatalf("X-Initiator = %q, want agent (tool role exists)", got)
	}
}

func TestApplyHeaders_XInitiator_InputArrayLastAssistantMessage(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Hi"}]},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Hello"}]}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "agent" {
		t.Fatalf("X-Initiator = %q, want agent (last role is assistant)", got)
	}
}

func TestApplyHeaders_XInitiator_InputArrayAgentWhenAnyAssistantMessage(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	body := []byte(`{"input":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I can help"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"Do X"}]}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "agent" {
		t.Fatalf("X-Initiator = %q, want agent (assistant role exists in input)", got)
	}
}

func TestApplyHeaders_XInitiator_InputArrayUserOnly(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"A"}]},{"type":"message","role":"user","content":[{"type":"input_text","text":"B"}]}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "user" {
		t.Fatalf("X-Initiator = %q, want user (user-only input array)", got)
	}
}

func TestApplyHeaders_XInitiator_InputArrayLastFunctionCallOutput(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Use tool"}]},{"type":"function_call","call_id":"c1","name":"Read","arguments":"{}"},{"type":"function_call_output","call_id":"c1","output":"ok"}]}`)
	e.applyHeaders(req, "token", body, false, false, nil)
	if got := req.Header.Get("X-Initiator"); got != "agent" {
		t.Fatalf("X-Initiator = %q, want agent (last item maps to tool role)", got)
	}
}

// --- Tests for x-github-api-version header (Problem M) ---

func TestApplyHeaders_GitHubAPIVersion(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	e.applyHeaders(req, "token", nil, false, false, nil)
	if got := req.Header.Get("X-Github-Api-Version"); got != "2025-10-01" {
		t.Fatalf("X-Github-Api-Version = %q, want 2025-10-01", got)
	}
}

func TestApplyHeaders_OpenAIIntent(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	e.applyHeaders(req, "token", nil, false, false, nil)
	if got := req.Header.Get("Openai-Intent"); got != "conversation-agent" {
		t.Fatalf("Openai-Intent = %q, want conversation-agent", got)
	}
}

func TestApplyHeaders_CopilotClientVersionHeaders(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	e.applyHeaders(req, "token", nil, false, false, nil)
	if got := req.Header.Get("User-Agent"); got != "GitHubCopilotChat/0.38.2" {
		t.Fatalf("User-Agent = %q, want GitHubCopilotChat/0.38.2", got)
	}
	if got := req.Header.Get("Editor-Version"); got != "vscode/1.110.1" {
		t.Fatalf("Editor-Version = %q, want vscode/1.110.1", got)
	}
	if got := req.Header.Get("Editor-Plugin-Version"); got != "copilot-chat/0.38.2" {
		t.Fatalf("Editor-Plugin-Version = %q, want copilot-chat/0.38.2", got)
	}
}

func TestApplyHeaders_StreamAcceptSSE(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	e.applyHeaders(req, "token", nil, true, false, nil)
	if got := req.Header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", got)
	}
}

func TestApplyHeaders_MessagesAddsAnthropicBeta(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	e.applyHeaders(req, "token", nil, false, true, nil)
	got := req.Header.Get("Anthropic-Beta")
	if !strings.Contains(got, "advanced-tool-use-2025-11-20") {
		t.Fatalf("Anthropic-Beta = %q, want to contain advanced-tool-use-2025-11-20", got)
	}
	if !strings.Contains(got, "interleaved-thinking-2025-05-14") {
		t.Fatalf("Anthropic-Beta = %q, want to contain interleaved-thinking-2025-05-14", got)
	}
}

func TestApplyHeaders_MessagesUserBetaMergedNotOverridden(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	incomingReq, _ := http.NewRequest(http.MethodPost, "http://local.test", nil)
	incomingReq.Header.Set("Anthropic-Beta", "my-custom-beta-2025-01-01")

	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = incomingReq

	outReq, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	outReq = outReq.WithContext(context.WithValue(outReq.Context(), "gin", ginCtx))

	e := &GitHubCopilotExecutor{}
	e.applyHeaders(outReq, "token", nil, false, true, nil)

	got := outReq.Header.Get("Anthropic-Beta")
	// 默认 beta 不能丢失
	if !strings.Contains(got, "advanced-tool-use-2025-11-20") {
		t.Fatalf("Anthropic-Beta = %q, want to retain advanced-tool-use-2025-11-20", got)
	}
	// 用户自定义 beta 必须存在
	if !strings.Contains(got, "my-custom-beta-2025-01-01") {
		t.Fatalf("Anthropic-Beta = %q, want to contain my-custom-beta-2025-01-01", got)
	}
}

func TestApplyHeaders_MessagesExtraBetasFromBodyMerged(t *testing.T) {
	t.Parallel()
	e := &GitHubCopilotExecutor{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.com", nil)
	extraBetas := []string{"files-api-2025-04-14", "advanced-tool-use-2025-11-20"} // 后者重复，应去重
	e.applyHeaders(req, "token", nil, false, true, extraBetas)
	got := req.Header.Get("Anthropic-Beta")
	// files-api beta 必须出现
	if !strings.Contains(got, "files-api-2025-04-14") {
		t.Fatalf("Anthropic-Beta = %q, want to contain files-api-2025-04-14", got)
	}
	// 重复 beta 不能导致重复值（advanced-tool-use 应只出现一次）
	count := strings.Count(got, "advanced-tool-use-2025-11-20")
	if count != 1 {
		t.Fatalf("Anthropic-Beta = %q, advanced-tool-use-2025-11-20 appears %d times, want 1", got, count)
	}
}

func TestApplyHeaders_NonMessagesDoesNotForwardAnthropicBeta(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	incomingReq, _ := http.NewRequest(http.MethodPost, "http://local.test", nil)
	incomingReq.Header.Set("Anthropic-Beta", "advanced-tool-use-2025-11-20")

	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = incomingReq

	outReq, _ := http.NewRequest(http.MethodPost, "https://example.com/chat/completions", nil)
	outReq = outReq.WithContext(context.WithValue(outReq.Context(), "gin", ginCtx))

	e := &GitHubCopilotExecutor{}
	e.applyHeaders(outReq, "token", nil, false, false, nil)
	if got := outReq.Header.Get("Anthropic-Beta"); got != "" {
		t.Fatalf("Anthropic-Beta = %q, want empty for non-messages", got)
	}
}

func TestApplyHeaders_AgentTaskIDFallbackToRequestID(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	incomingReq, _ := http.NewRequest(http.MethodPost, "http://local.test", nil)
	incomingReq.Header.Set("X-Request-Id", "req-id-fallback-1")

	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = incomingReq

	outReq, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	outReq = outReq.WithContext(context.WithValue(outReq.Context(), "gin", ginCtx))

	e := &GitHubCopilotExecutor{}
	e.applyHeaders(outReq, "token", nil, false, true, nil)
	if got := outReq.Header.Get("X-Agent-Task-Id"); got != "req-id-fallback-1" {
		t.Fatalf("X-Agent-Task-Id = %q, want req-id-fallback-1", got)
	}
}

func TestApplyHeaders_ForwardStrictCopilotContextHeaders(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	incomingReq, _ := http.NewRequest(http.MethodPost, "http://local.test", nil)
	incomingReq.Header.Set("Vscode-Abexpcontext", "abexp-ctx")
	incomingReq.Header.Set("Vscode-Machineid", "machine-id-1")
	incomingReq.Header.Set("Vscode-Sessionid", "session-id-1")
	incomingReq.Header.Set("X-Agent-Task-Id", "task-id-1")
	incomingReq.Header.Set("X-Interaction-Id", "interaction-id-1")
	incomingReq.Header.Set("X-Interaction-Type", "conversation-agent")
	incomingReq.Header.Set("X-Vscode-User-Agent-Library-Version", "electron-fetch")
	incomingReq.Header.Set("Sec-Fetch-Site", "none")
	incomingReq.Header.Set("Sec-Fetch-Mode", "no-cors")
	incomingReq.Header.Set("Sec-Fetch-Dest", "empty")
	incomingReq.Header.Set("Priority", "u=4, i")
	incomingReq.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")
	incomingReq.Header.Set("X-Request-Id", "req-id-1")
	incomingReq.Header.Set("X-Initiator", "user")

	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = incomingReq

	outReq, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	outReq = outReq.WithContext(context.WithValue(outReq.Context(), "gin", ginCtx))

	e := &GitHubCopilotExecutor{}
	// X-Initiator must be generated locally from payload roles.
	e.applyHeaders(outReq, "token", []byte(`{"messages":[{"role":"assistant","content":"ok"}]}`), false, true, nil)

	checks := map[string]string{
		"Vscode-Abexpcontext":                 "abexp-ctx",
		"Vscode-Machineid":                    "machine-id-1",
		"Vscode-Sessionid":                    "session-id-1",
		"X-Agent-Task-Id":                     "task-id-1",
		"X-Interaction-Id":                    "interaction-id-1",
		"X-Interaction-Type":                  "conversation-agent",
		"X-Vscode-User-Agent-Library-Version": "electron-fetch",
		"Sec-Fetch-Site":                      "none",
		"Sec-Fetch-Mode":                      "no-cors",
		"Sec-Fetch-Dest":                      "empty",
		"Priority":                            "u=4, i",
		"Accept-Encoding":                     "gzip, deflate, br, zstd",
		"X-Request-Id":                        "req-id-1",
		"X-Initiator":                         "agent",
	}
	for k, v := range checks {
		if got := outReq.Header.Get(k); got != v {
			t.Fatalf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestApplyHeaders_XInitiator_NotForwardedFromIncomingRequest(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	incomingReq, _ := http.NewRequest(http.MethodPost, "http://local.test", nil)
	incomingReq.Header.Set("X-Initiator", "agent")

	w := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(w)
	ginCtx.Request = incomingReq

	outReq, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/messages", nil)
	outReq = outReq.WithContext(context.WithValue(outReq.Context(), "gin", ginCtx))

	e := &GitHubCopilotExecutor{}
	// payload is user-only, so locally generated value must be user
	e.applyHeaders(outReq, "token", []byte(`{"messages":[{"role":"user","content":"hello"}]}`), false, true, nil)

	if got := outReq.Header.Get("X-Initiator"); got != "user" {
		t.Fatalf("X-Initiator = %q, want user", got)
	}
}

// --- Tests for vision detection (Problem P) ---

func TestDetectVisionContent_WithImageURL(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,abc"}}]}]}`)
	if !detectVisionContent(body) {
		t.Fatal("expected vision content to be detected")
	}
}

func TestDetectVisionContent_WithImageType(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image","source":{"data":"abc","media_type":"image/png"}}]}]}`)
	if !detectVisionContent(body) {
		t.Fatal("expected image type to be detected")
	}
}

func TestDetectVisionContent_NoVision(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)
	if detectVisionContent(body) {
		t.Fatal("expected no vision content")
	}
}

func TestDetectVisionContent_NoMessages(t *testing.T) {
	t.Parallel()
	// After Responses API normalization, messages is removed — detection should return false
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`)
	if detectVisionContent(body) {
		t.Fatal("expected no vision content when messages field is absent")
	}
}

// TestChatCompletionsBodyDoesNotInjectStreamOptions verifies the actual
// ExecuteStream production path does not inject stream_options into a
// chat/completions upstream request body.
func TestChatCompletionsBodyDoesNotInjectStreamOptions(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds:        17,
		ResponseHeaderTimeoutSeconds: 47,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)
	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	for range stream.Chunks {
	}

	if len(capturedBody) == 0 {
		t.Fatal("expected upstream request body to be captured")
	}
	if !gjson.GetBytes(capturedBody, "stream").Bool() {
		t.Fatalf("stream = %s, want true", gjson.GetBytes(capturedBody, "stream").Raw)
	}
	if gjson.GetBytes(capturedBody, "stream_options").Exists() {
		t.Fatalf("chat/completions body must NOT contain stream_options, got: %s", capturedBody)
	}
}

func TestGitHubCopilotMessagesStream_ClaudeToClaude_AppendsNewlinePerLine(t *testing.T) {
	// This simulates the upstream Copilot /v1/messages SSE where the stream is already
	// in Claude format. When the incoming request is also Claude, the executor should
	// forward the SSE line-by-line while preserving SSE framing (i.e., add back '\n'
	// removed by bufio.Scanner).
	upstream := strings.Join([]string{
		"event: message_start",
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-opus-4-6\",\"content\":[],\"stop_reason\":null,\"stop_sequence\":null,\"usage\":{\"input_tokens\":0,\"output_tokens\":0}}}",
		"",
		"event: content_block_delta",
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}",
		"",
		"event: message_stop",
		"data: {\"type\":\"message_stop\"}",
		"",
		"data: [DONE]",
		"",
	}, "\n")

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstream)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds:        17,
		ResponseHeaderTimeoutSeconds: 47,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"claude-opus-4-6","max_tokens":16,"messages":[{"role":"user","content":"hello"}],"stream":true}`)

	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "claude-opus-4-6",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("claude"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	// The first chunk should be a complete SSE line ending with '\n'.
	first, ok := <-stream.Chunks
	if !ok {
		t.Fatal("expected at least one stream chunk")
	}
	if first.Err != nil {
		t.Fatalf("first chunk error = %v", first.Err)
	}
	if !bytes.HasSuffix(first.Payload, []byte("\n")) {
		t.Fatalf("first chunk must end with newline; got %q", string(first.Payload))
	}
}

func TestGitHubCopilotResponsesStream_OpenAIResponseToOpenAIResponse_ForwardsSSEVerbatim(t *testing.T) {
	// Copilot /responses uses SSE with explicit event lines. When the downstream client
	// is also using OpenAI Responses format (openai-response -> openai-response), the
	// executor should forward SSE verbatim (preserving event lines + blank delimiters)
	// instead of re-translating per-line.
	upstream := strings.Join([]string{
		"event: response.created",
		"",
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"gpt-5.4\"}}",
		"",
		"event: response.output_item.added",
		"",
		"data: {\"type\":\"response.output_item.added\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"r1\",\"summary\":[]}}",
		"",
		"event: response.output_item.done",
		"",
		"data: {\"type\":\"response.output_item.done\",\"output_index\":0,\"item\":{\"type\":\"reasoning\",\"id\":\"r1\",\"summary\":[]}}",
		"",
		"event: response.completed",
		"",
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\"}}",
		"",
	}, "\n")

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstream)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds:        17,
		ResponseHeaderTimeoutSeconds: 47,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)

	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai-response"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var got bytes.Buffer
	for chunk := range stream.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		got.Write(chunk.Payload)
	}

	if got.String() != upstream {
		t.Fatalf("forwarded SSE mismatch\n--- got ---\n%s\n--- want ---\n%s", got.String(), upstream)
	}
}

func TestGitHubCopilotResponsesStream_OpenAIResponseToOpenAIResponse_FixesMismatchedReasoningIDs(t *testing.T) {
	// Copilot sometimes returns reasoning items where the item.id in
	// response.output_item.added differs from the item.id in
	// response.output_item.done (and in response.completed output array).
	// The ai-sdk uses item.id as a map key: it stores reasoning state on
	// "added" and looks it up on "done". Mismatched IDs cause a crash:
	//   TypeError: activeReasoningPart.summaryParts
	// The proxy must normalise the IDs so "done" + "completed" use the
	// same ID that was seen in "added".
	addedID := "ZkFscWKLVSloolvA6KsXSaEQ_original_added_id"
	doneID := "T5Lp1VZ4fsJ9rZX7k0I_DIFFERENT_done_id"

	upstream := strings.Join([]string{
		"event: response.created",
		"",
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.4","output":[]}}`,
		"",
		"event: response.output_item.added",
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"` + addedID + `","summary":[]}}`,
		"",
		"event: response.output_item.done",
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"` + doneID + `","summary":[]}}`,
		"",
		"event: response.output_item.added",
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg1","role":"assistant","content":[]}}`,
		"",
		"event: response.content_part.added",
		"",
		`data: {"type":"response.content_part.added","output_index":1,"content_index":0,"part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		"",
		`data: {"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"hi"}`,
		"",
		"event: response.output_text.done",
		"",
		`data: {"type":"response.output_text.done","output_index":1,"content_index":0,"text":"hi"}`,
		"",
		"event: response.content_part.done",
		"",
		`data: {"type":"response.content_part.done","output_index":1,"content_index":0,"part":{"type":"output_text","text":"hi"}}`,
		"",
		"event: response.output_item.done",
		"",
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`,
		"",
		"event: response.completed",
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"` + doneID + `","summary":[]},{"type":"message","id":"msg1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}}`,
		"",
	}, "\n")

	// Expected: the proxy replaces doneID with addedID everywhere.
	want := strings.Join([]string{
		"event: response.created",
		"",
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.4","output":[]}}`,
		"",
		"event: response.output_item.added",
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"reasoning","id":"` + addedID + `","summary":[]}}`,
		"",
		"event: response.output_item.done",
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"reasoning","id":"` + addedID + `","summary":[]}}`,
		"",
		"event: response.output_item.added",
		"",
		`data: {"type":"response.output_item.added","output_index":1,"item":{"type":"message","id":"msg1","role":"assistant","content":[]}}`,
		"",
		"event: response.content_part.added",
		"",
		`data: {"type":"response.content_part.added","output_index":1,"content_index":0,"part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		"",
		`data: {"type":"response.output_text.delta","output_index":1,"content_index":0,"delta":"hi"}`,
		"",
		"event: response.output_text.done",
		"",
		`data: {"type":"response.output_text.done","output_index":1,"content_index":0,"text":"hi"}`,
		"",
		"event: response.content_part.done",
		"",
		`data: {"type":"response.content_part.done","output_index":1,"content_index":0,"part":{"type":"output_text","text":"hi"}}`,
		"",
		"event: response.output_item.done",
		"",
		`data: {"type":"response.output_item.done","output_index":1,"item":{"type":"message","id":"msg1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}}`,
		"",
		"event: response.completed",
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"reasoning","id":"` + addedID + `","summary":[]},{"type":"message","id":"msg1","role":"assistant","content":[{"type":"output_text","text":"hi"}]}]}}`,
		"",
	}, "\n")

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstream)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds:        17,
		ResponseHeaderTimeoutSeconds: 47,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)

	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai-response"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var got bytes.Buffer
	for chunk := range stream.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		got.Write(chunk.Payload)
	}

	if got.String() != want {
		t.Fatalf("reasoning ID fixup mismatch\n--- got ---\n%s\n--- want ---\n%s", got.String(), want)
	}
}

func TestGitHubCopilotResponsesStream_OpenAIResponseToOpenAIResponse_FixesMismatchedTextPartIDs(t *testing.T) {
	// Copilot returns different item_id values for content_part.added vs
	// output_text.delta (and done) events for the same text content part.
	// Worse, each individual output_text.delta event can carry yet another
	// different item_id.  The @ai-sdk/openai Responses provider registers a
	// text part on content_part.added using its item_id, then looks it up by
	// item_id in output_text.delta.  Mismatched IDs cause:
	//   "text part <id> not found"
	// The proxy must normalise the IDs so delta/done events use the same
	// item_id that was seen in content_part.added.
	contentPartAddedItemID := "K_UlBuLO_content_part_added_item_id"
	deltaItemID1 := "c4Nz2lFt_delta_item_id_first"
	deltaItemID2 := "K8MR66lL_delta_item_id_second"
	outputTextDoneItemID := "hBY1Lidc_output_text_done_item_id"
	contentPartDoneItemID := "1R0AbnAQ_content_part_done_item_id"

	upstream := strings.Join([]string{
		"event: response.created",
		"",
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.4","output":[]}}`,
		"",
		"event: response.output_item.added",
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg-added","role":"assistant","content":[]}}`,
		"",
		"event: response.content_part.added",
		"",
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"item_id":"` + contentPartAddedItemID + `","part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		"",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"` + deltaItemID1 + `","delta":"hello"}`,
		"",
		"event: response.output_text.delta",
		"",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"` + deltaItemID2 + `","delta":" world"}`,
		"",
		"event: response.output_text.done",
		"",
		`data: {"type":"response.output_text.done","output_index":0,"content_index":0,"item_id":"` + outputTextDoneItemID + `","text":"hello world"}`,
		"",
		"event: response.content_part.done",
		"",
		`data: {"type":"response.content_part.done","output_index":0,"content_index":0,"item_id":"` + contentPartDoneItemID + `","part":{"type":"output_text","text":"hello world"}}`,
		"",
		"event: response.output_item.done",
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg-done","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}}`,
		"",
		"event: response.completed",
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"message","id":"msg-done","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}]}}`,
		"",
	}, "\n")

	// Expected: the proxy normalises all item_id fields in delta/done events
	// to the item_id seen in content_part.added.
	want := strings.Join([]string{
		"event: response.created",
		"",
		`data: {"type":"response.created","response":{"id":"resp_1","model":"gpt-5.4","output":[]}}`,
		"",
		"event: response.output_item.added",
		"",
		`data: {"type":"response.output_item.added","output_index":0,"item":{"type":"message","id":"msg-added","role":"assistant","content":[]}}`,
		"",
		"event: response.content_part.added",
		"",
		`data: {"type":"response.content_part.added","output_index":0,"content_index":0,"item_id":"` + contentPartAddedItemID + `","part":{"type":"output_text","text":""}}`,
		"",
		"event: response.output_text.delta",
		"",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"` + contentPartAddedItemID + `","delta":"hello"}`,
		"",
		"event: response.output_text.delta",
		"",
		`data: {"type":"response.output_text.delta","output_index":0,"content_index":0,"item_id":"` + contentPartAddedItemID + `","delta":" world"}`,
		"",
		"event: response.output_text.done",
		"",
		`data: {"type":"response.output_text.done","output_index":0,"content_index":0,"item_id":"` + contentPartAddedItemID + `","text":"hello world"}`,
		"",
		"event: response.content_part.done",
		"",
		`data: {"type":"response.content_part.done","output_index":0,"content_index":0,"item_id":"` + contentPartAddedItemID + `","part":{"type":"output_text","text":"hello world"}}`,
		"",
		"event: response.output_item.done",
		"",
		`data: {"type":"response.output_item.done","output_index":0,"item":{"type":"message","id":"msg-done","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}}`,
		"",
		"event: response.completed",
		"",
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","output":[{"type":"message","id":"msg-done","role":"assistant","content":[{"type":"output_text","text":"hello world"}]}]}}`,
		"",
	}, "\n")

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstream)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds:        17,
		ResponseHeaderTimeoutSeconds: 47,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-5.4","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}]}`)

	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-5.4",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai-response"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}

	var got bytes.Buffer
	for chunk := range stream.Chunks {
		if chunk.Err != nil {
			t.Fatalf("stream chunk error = %v", chunk.Err)
		}
		got.Write(chunk.Payload)
	}

	if got.String() != want {
		t.Fatalf("text part ID fixup mismatch\n--- got ---\n%s\n--- want ---\n%s", got.String(), want)
	}
}

func TestGitHubCopilotExecute_BetasExtractedFromBodyIntoHeader(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header
	var capturedBody []byte
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		capturedHeaders = req.Header.Clone()
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4.6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds: 11, ResponseHeaderTimeoutSeconds: 41,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	// payload 中包含 betas 字段
	payload := []byte(`{"model":"claude-opus-4.6","max_tokens":128,"messages":[{"role":"user","content":"hi"}],"betas":["files-api-2025-04-14"]}`)

	_, err := e.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "claude-opus-4.6",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	// 验证 betas 字段已从 body 提取到 header
	betaHeader := capturedHeaders.Get("Anthropic-Beta")
	if !strings.Contains(betaHeader, "files-api-2025-04-14") {
		t.Fatalf("Anthropic-Beta = %q, want to contain files-api-2025-04-14 (extracted from body)", betaHeader)
	}
	// 验证 betas 字段已从请求体中移除（不能同时出现在 body 和 header）
	if gjson.GetBytes(capturedBody, "betas").Exists() {
		t.Fatalf("request body still contains 'betas' field after extraction: %s", capturedBody)
	}
}

func TestGitHubCopilotExecute_ThinkingDisabledWhenToolChoiceForced(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"claude-opus-4.6","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds: 12, ResponseHeaderTimeoutSeconds: 42,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	// thinking enabled + tool_choice forced → thinking 应被禁用
	payload := []byte(`{"model":"claude-opus-4.6","max_tokens":128,"thinking":{"type":"enabled","budget_tokens":8000},"tool_choice":{"type":"tool","name":"my_tool"},"messages":[{"role":"user","content":"hi"}]}`)

	_, err := e.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "claude-opus-4.6",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat: sdktranslator.FromString("claude"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	thinkingResult := gjson.GetBytes(capturedBody, "thinking")
	if thinkingResult.Exists() {
		thinkingType := thinkingResult.Get("type").String()
		if thinkingType == "enabled" || thinkingType == "adaptive" {
			t.Fatalf("thinking should be absent or disabled when tool_choice forces tool use, got thinking=%s", thinkingResult.Raw)
		}
	}
}

func TestGitHubCopilotExecuteStream_BetasExtractedFromBodyIntoHeader(t *testing.T) {
	t.Parallel()

	// Minimal SSE response in Claude messages format
	upstream := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-4.6","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		"",
		"event: content_block_stop",
		`data: {"type":"content_block_stop","index":0}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
		"data: [DONE]",
		"",
	}, "\n")

	var capturedHeaders http.Header
	var capturedBody []byte
	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(req *http.Request) (*http.Response, error) {
		capturedHeaders = req.Header.Clone()
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader(upstream)),
		}, nil
	}))

	e := NewGitHubCopilotExecutor(&config.Config{SDKConfig: config.SDKConfig{UpstreamTimeouts: config.UpstreamTimeouts{
		ConnectTimeoutSeconds: 13, ResponseHeaderTimeoutSeconds: 43,
	}}})
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	// payload 中包含 betas 字段
	payload := []byte(`{"model":"claude-opus-4.6","max_tokens":128,"messages":[{"role":"user","content":"hi"}],"betas":["files-api-2025-04-14"]}`)

	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "claude-opus-4.6",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("claude"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream() error = %v", err)
	}
	// 消耗所有流数据以确保请求已完成
	for range stream.Chunks {
	}

	// 验证 betas 字段已从 body 提取到 header
	betaHeader := capturedHeaders.Get("Anthropic-Beta")
	if !strings.Contains(betaHeader, "files-api-2025-04-14") {
		t.Fatalf("Anthropic-Beta = %q, want to contain files-api-2025-04-14 (extracted from body)", betaHeader)
	}
	// 验证 betas 字段已从请求体中移除（不能同时出现在 body 和 header）
	if gjson.GetBytes(capturedBody, "betas").Exists() {
		t.Fatalf("request body still contains 'betas' field after extraction: %s", capturedBody)
	}
}

// --- Tests for injectFakeAssistantMessage ---

func TestInjectFakeAssistantMessage_MessagesUserOnly(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	result := injectFakeAssistantMessage(body, "OK.", false)
	msgs := gjson.GetBytes(result, "messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[0].Get("role").String() != "assistant" {
		t.Fatalf("first injected message role = %q, want assistant", msgs[0].Get("role").String())
	}
	if msgs[1].Get("role").String() != "user" {
		t.Fatalf("last message role = %q, want user", msgs[1].Get("role").String())
	}
}

func TestInjectFakeAssistantMessage_MessagesMultiUser(t *testing.T) {
	t.Parallel()
	// [user, user] → [user, assistant, user]
	body := []byte(`{"messages":[{"role":"user","content":"first"},{"role":"user","content":"second"}]}`)
	result := injectFakeAssistantMessage(body, "OK.", false)
	msgs := gjson.GetBytes(result, "messages").Array()
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	if msgs[1].Get("role").String() != "assistant" {
		t.Fatalf("msgs[1].role = %q, want assistant", msgs[1].Get("role").String())
	}
	if msgs[2].Get("role").String() != "user" {
		t.Fatalf("msgs[2].role = %q, want user", msgs[2].Get("role").String())
	}
}

func TestInjectFakeAssistantMessage_MessagesWithSystem(t *testing.T) {
	t.Parallel()
	// [system, user] → [system, assistant, user]
	body := []byte(`{"messages":[{"role":"system","content":"sys"},{"role":"user","content":"hello"}]}`)
	result := injectFakeAssistantMessage(body, "OK.", false)
	msgs := gjson.GetBytes(result, "messages").Array()
	if len(msgs) != 3 {
		t.Fatalf("want 3 messages, got %d", len(msgs))
	}
	if msgs[1].Get("role").String() != "assistant" {
		t.Fatalf("msgs[1].role = %q, want assistant", msgs[1].Get("role").String())
	}
}

func TestInjectFakeAssistantMessage_CustomContent(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	result := injectFakeAssistantMessage(body, "Understood.", false)
	msgs := gjson.GetBytes(result, "messages").Array()
	if got := msgs[0].Get("content").String(); got != "Understood." {
		t.Fatalf("injected content = %q, want Understood.", got)
	}
}

func TestInjectFakeAssistantMessage_DefaultContentWhenEmpty(t *testing.T) {
	t.Parallel()
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	result := injectFakeAssistantMessage(body, "", false)
	msgs := gjson.GetBytes(result, "messages").Array()
	if got := msgs[0].Get("content").String(); got != "OK." {
		t.Fatalf("injected content = %q, want OK. (default)", got)
	}
}

func TestInjectFakeAssistantMessage_ResponsesInputFormat(t *testing.T) {
	t.Parallel()
	body := []byte(`{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"Hi"}]}]}`)
	result := injectFakeAssistantMessage(body, "OK.", true)
	items := gjson.GetBytes(result, "input").Array()
	if len(items) != 2 {
		t.Fatalf("want 2 input items, got %d", len(items))
	}
	if items[0].Get("role").String() != "assistant" {
		t.Fatalf("items[0].role = %q, want assistant", items[0].Get("role").String())
	}
	if items[0].Get("type").String() != "message" {
		t.Fatalf("items[0].type = %q, want message", items[0].Get("type").String())
	}
	if got := items[0].Get("content.0.type").String(); got != "output_text" {
		t.Fatalf("items[0].content[0].type = %q, want output_text", got)
	}
}

func TestInjectFakeAssistantMessage_EmptyBody(t *testing.T) {
	t.Parallel()
	result := injectFakeAssistantMessage([]byte{}, "OK.", false)
	if len(result) != 0 {
		t.Fatalf("empty body should be returned as-is, got %q", result)
	}
}

func TestInjectFakeAssistantMessage_NoUserMessage_AppendsToEnd(t *testing.T) {
	t.Parallel()
	// No user message: fallback - append assistant to end
	body := []byte(`{"messages":[{"role":"system","content":"sys"}]}`)
	result := injectFakeAssistantMessage(body, "OK.", false)
	msgs := gjson.GetBytes(result, "messages").Array()
	if len(msgs) != 2 {
		t.Fatalf("want 2 messages, got %d", len(msgs))
	}
	if msgs[1].Get("role").String() != "assistant" {
		t.Fatalf("msgs[1].role = %q, want assistant", msgs[1].Get("role").String())
	}
}

func TestInjectFakeAssistantMessage_XInitiatorBecomesAgent(t *testing.T) {
	t.Parallel()
	// After injection, containsAgentConversationRole should return true.
	body := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	injected := injectFakeAssistantMessage(body, "OK.", false)
	if !containsAgentConversationRole(injected) {
		t.Fatal("containsAgentConversationRole = false after injection, want true")
	}
}

// --- Integration tests for Execute with force-agent-initiator ---

func TestExecute_ForceAgentInitiator_InjectsAssistantAndSetsHeader(t *testing.T) {
	t.Parallel()

	var capturedBody []byte
	var capturedInitiator string

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(r *http.Request) (*http.Response, error) {
		capturedBody, _ = io.ReadAll(r.Body)
		capturedInitiator = r.Header.Get("X-Initiator")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"x","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)),
		}, nil
	}))

	cfg := &config.Config{}
	cfg.GitHubCopilot.ForceAgentInitiator = true
	cfg.GitHubCopilot.FakeAssistantContent = "Understood."
	cfg.SDKConfig.UpstreamTimeouts.ConnectTimeoutSeconds = 91
	cfg.SDKConfig.UpstreamTimeouts.ResponseHeaderTimeoutSeconds = 92

	e := NewGitHubCopilotExecutor(cfg)
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}

	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)

	_, err := e.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if capturedInitiator != "agent" {
		t.Fatalf("X-Initiator = %q, want agent", capturedInitiator)
	}
	msgs := gjson.GetBytes(capturedBody, "messages").Array()
	hasAssistant := false
	for _, m := range msgs {
		if m.Get("role").String() == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		t.Fatalf("upstream body has no assistant message, body = %s", capturedBody)
	}
}

func TestExecute_ForceAgentInitiator_Disabled_NoInjection(t *testing.T) {
	t.Parallel()

	var capturedInitiator string

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(r *http.Request) (*http.Response, error) {
		capturedInitiator = r.Header.Get("X-Initiator")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"x","choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1}}`)),
		}, nil
	}))

	cfg := &config.Config{}
	cfg.GitHubCopilot.ForceAgentInitiator = false
	cfg.SDKConfig.UpstreamTimeouts.ConnectTimeoutSeconds = 93
	cfg.SDKConfig.UpstreamTimeouts.ResponseHeaderTimeoutSeconds = 94

	e := NewGitHubCopilotExecutor(cfg)
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}

	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)

	_, err := e.Execute(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if capturedInitiator != "user" {
		t.Fatalf("X-Initiator = %q, want user (feature disabled)", capturedInitiator)
	}
}

// --- Integration test for ExecuteStream with force-agent-initiator ---

func TestExecuteStream_ForceAgentInitiator_InjectsAssistant(t *testing.T) {
	t.Parallel()

	var capturedInitiator string

	ctx := context.WithValue(context.Background(), "cliproxy.roundtripper", testRoundTripper(func(r *http.Request) (*http.Response, error) {
		capturedInitiator = r.Header.Get("X-Initiator")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:       io.NopCloser(strings.NewReader("data: [DONE]\n\n")),
		}, nil
	}))

	cfg := &config.Config{}
	cfg.GitHubCopilot.ForceAgentInitiator = true
	cfg.SDKConfig.UpstreamTimeouts.ConnectTimeoutSeconds = 95
	cfg.SDKConfig.UpstreamTimeouts.ResponseHeaderTimeoutSeconds = 96

	e := NewGitHubCopilotExecutor(cfg)
	e.cache["gh-access"] = &cachedAPIToken{
		token:       "copilot-api-token",
		apiEndpoint: "https://api.business.githubcopilot.com",
		expiresAt:   time.Now().Add(time.Hour),
	}

	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gh-access"}}
	payload := []byte(`{"model":"gpt-4o","messages":[{"role":"user","content":"hello"}]}`)

	stream, err := e.ExecuteStream(ctx, auth, cliproxyexecutor.Request{
		Model:   "gpt-4o",
		Payload: bytes.Clone(payload),
	}, cliproxyexecutor.Options{
		SourceFormat:    sdktranslator.FromString("openai"),
		OriginalRequest: bytes.Clone(payload),
	})
	if err != nil {
		t.Fatalf("ExecuteStream failed: %v", err)
	}
	if stream != nil {
		for range stream.Chunks {
		}
	}
	if capturedInitiator != "agent" {
		t.Fatalf("X-Initiator = %q, want agent", capturedInitiator)
	}
}
