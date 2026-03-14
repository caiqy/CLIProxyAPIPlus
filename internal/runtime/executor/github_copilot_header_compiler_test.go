package executor

import (
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestHeaderCompiler_MapsAuthMetadataContextHeaders(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(false)
	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"editor_device_id":    " editor-meta ",
		"vscode_abexpcontext": "abexp-meta",
		"vscode_machineid":    "",
	}}

	headers := compiler.Compile(http.Header{}, auth, githubCopilotHeaderCompileContext{})

	if got := headers.Get("Editor-Device-Id"); got != "editor-meta" {
		t.Fatalf("Editor-Device-Id = %q, want editor-meta", got)
	}
	if got := headers.Get("Vscode-Abexpcontext"); got != "abexp-meta" {
		t.Fatalf("Vscode-Abexpcontext = %q, want abexp-meta", got)
	}
	if got := headers.Get("Vscode-Machineid"); got != "" {
		t.Fatalf("Vscode-Machineid = %q, want empty", got)
	}
}

func TestHeaderCompiler_GeneratesUUIDv7RequestID(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(false)
	headers := compiler.Compile(http.Header{}, nil, githubCopilotHeaderCompileContext{})

	requestID := headers.Get("X-Request-Id")
	if requestID == "" {
		t.Fatal("X-Request-Id is empty")
	}
	parsed, err := uuid.Parse(requestID)
	if err != nil {
		t.Fatalf("X-Request-Id parse error: %v", err)
	}
	if got, want := parsed.Version(), uuid.Version(7); got != want {
		t.Fatalf("X-Request-Id version = %d, want %d", got, want)
	}
}

func TestHeaderCompiler_TaskIDEqualsRequestID(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(false)
	headers := compiler.Compile(http.Header{}, nil, githubCopilotHeaderCompileContext{})

	requestID := headers.Get("X-Request-Id")
	taskID := headers.Get("X-Agent-Task-Id")
	if taskID == "" {
		t.Fatal("X-Agent-Task-Id is empty")
	}
	if taskID != requestID {
		t.Fatalf("X-Agent-Task-Id = %q, want %q", taskID, requestID)
	}
}

func TestHeaderCompiler_SetsFixedHeaders(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(false)
	headers := compiler.Compile(http.Header{}, nil, githubCopilotHeaderCompileContext{})

	checks := map[string]string{
		"X-Interaction-Type":                  "conversation-agent",
		"X-Vscode-User-Agent-Library-Version": "electron-fetch",
		"Sec-Fetch-Site":                      "none",
		"Sec-Fetch-Mode":                      "no-cors",
		"Sec-Fetch-Dest":                      "empty",
		"Priority":                            "u=4, i",
		"Accept-Encoding":                     "gzip, deflate, br, zstd",
	}
	for key, want := range checks {
		if got := headers.Get(key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestHeaderCompiler_StrictIgnoresIncomingHeaders(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(true)
	incoming := http.Header{}
	incoming.Set("X-Interaction-Type", "incoming-interaction")
	incoming.Set("X-Vscode-User-Agent-Library-Version", "incoming-lib")
	incoming.Set("Sec-Fetch-Site", "incoming-site")
	incoming.Set("Sec-Fetch-Mode", "incoming-mode")
	incoming.Set("Sec-Fetch-Dest", "incoming-dest")
	incoming.Set("Priority", "incoming-priority")
	incoming.Set("Accept-Encoding", "incoming-encoding")
	incoming.Set("X-Request-Id", "incoming-request-id")
	incoming.Set("X-Agent-Task-Id", "incoming-task-id")
	incoming.Set("User-Agent", "incoming-user-agent")
	incoming.Set("Editor-Version", "incoming-editor-version")
	incoming.Set("Editor-Plugin-Version", "incoming-plugin-version")
	incoming.Set("Anthropic-Beta", "incoming-beta")
	incoming.Set("Editor-Device-Id", "incoming-editor-device")
	incoming.Set("Vscode-Abexpcontext", "incoming-abexp")
	incoming.Set("Vscode-Machineid", "incoming-machine")

	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"editor_device_id":    "meta-editor-device",
		"vscode_abexpcontext": "meta-abexp",
		"vscode_machineid":    "meta-machine",
	}}

	headers := compiler.Compile(incoming, auth, githubCopilotHeaderCompileContext{})

	if got := headers.Get("X-Interaction-Type"); got != "conversation-agent" {
		t.Fatalf("X-Interaction-Type = %q, want conversation-agent", got)
	}
	if got := headers.Get("X-Vscode-User-Agent-Library-Version"); got != "electron-fetch" {
		t.Fatalf("X-Vscode-User-Agent-Library-Version = %q, want electron-fetch", got)
	}
	if got := headers.Get("Sec-Fetch-Site"); got != "none" {
		t.Fatalf("Sec-Fetch-Site = %q, want none", got)
	}
	if got := headers.Get("Sec-Fetch-Mode"); got != "no-cors" {
		t.Fatalf("Sec-Fetch-Mode = %q, want no-cors", got)
	}
	if got := headers.Get("Sec-Fetch-Dest"); got != "empty" {
		t.Fatalf("Sec-Fetch-Dest = %q, want empty", got)
	}
	if got := headers.Get("Priority"); got != "u=4, i" {
		t.Fatalf("Priority = %q, want u=4, i", got)
	}
	if got := headers.Get("Accept-Encoding"); got != "gzip, deflate, br, zstd" {
		t.Fatalf("Accept-Encoding = %q, want gzip, deflate, br, zstd", got)
	}

	requestID := headers.Get("X-Request-Id")
	if requestID == "" || requestID == "incoming-request-id" {
		t.Fatalf("X-Request-Id = %q, want generated UUIDv7", requestID)
	}
	if got := headers.Get("X-Agent-Task-Id"); got != requestID {
		t.Fatalf("X-Agent-Task-Id = %q, want %q", got, requestID)
	}
	if got := headers.Get("User-Agent"); got != copilotUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, copilotUserAgent)
	}
	if got := headers.Get("Editor-Version"); got != copilotEditorVersion {
		t.Fatalf("Editor-Version = %q, want %q", got, copilotEditorVersion)
	}
	if got := headers.Get("Editor-Plugin-Version"); got != copilotPluginVersion {
		t.Fatalf("Editor-Plugin-Version = %q, want %q", got, copilotPluginVersion)
	}
	if got := headers.Get("Anthropic-Beta"); got != copilotAnthropicBeta {
		t.Fatalf("Anthropic-Beta = %q, want %q", got, copilotAnthropicBeta)
	}

	if got := headers.Get("Editor-Device-Id"); got != "meta-editor-device" {
		t.Fatalf("Editor-Device-Id = %q, want meta-editor-device", got)
	}
	if got := headers.Get("Vscode-Abexpcontext"); got != "meta-abexp" {
		t.Fatalf("Vscode-Abexpcontext = %q, want meta-abexp", got)
	}
	if got := headers.Get("Vscode-Machineid"); got != "meta-machine" {
		t.Fatalf("Vscode-Machineid = %q, want meta-machine", got)
	}
}

func TestHeaderCompiler_ConfigHeaders_WhenConfigured(t *testing.T) {
	t.Parallel()

	policy := config.GitHubCopilotHeaderPolicyConfig{
		UserAgent:           "  GitHubCopilotChat/9.9.9  ",
		EditorVersion:       "vscode/9.9.9",
		EditorPluginVersion: " copilot-chat/9.9.9 ",
		AnthropicBeta:       "beta-a,beta-b",
	}
	compiler := newGitHubCopilotHeaderCompiler(false, policy)
	incoming := http.Header{}
	incoming.Set("User-Agent", "incoming-user-agent")
	incoming.Set("Editor-Version", "incoming-editor-version")
	incoming.Set("Editor-Plugin-Version", "incoming-plugin-version")
	incoming.Set("Anthropic-Beta", "incoming-beta")

	headers := compiler.Compile(incoming, nil, githubCopilotHeaderCompileContext{})

	if got := headers.Get("User-Agent"); got != "GitHubCopilotChat/9.9.9" {
		t.Fatalf("User-Agent = %q, want GitHubCopilotChat/9.9.9", got)
	}
	if got := headers.Get("Editor-Version"); got != "vscode/9.9.9" {
		t.Fatalf("Editor-Version = %q, want vscode/9.9.9", got)
	}
	if got := headers.Get("Editor-Plugin-Version"); got != "copilot-chat/9.9.9" {
		t.Fatalf("Editor-Plugin-Version = %q, want copilot-chat/9.9.9", got)
	}
	if got := headers.Get("Anthropic-Beta"); got != "beta-a,beta-b" {
		t.Fatalf("Anthropic-Beta = %q, want beta-a,beta-b", got)
	}
}

func TestHeaderCompiler_ConfigHeaders_FallbackWhenMissing(t *testing.T) {
	t.Parallel()

	policy := config.GitHubCopilotHeaderPolicyConfig{
		UserAgent:           "",
		EditorVersion:       "   ",
		EditorPluginVersion: "copilot-chat/9.9.9",
		AnthropicBeta:       "",
	}
	compiler := newGitHubCopilotHeaderCompiler(false, policy)
	headers := compiler.Compile(http.Header{}, nil, githubCopilotHeaderCompileContext{})

	if got := headers.Get("User-Agent"); got != copilotUserAgent {
		t.Fatalf("User-Agent = %q, want %q", got, copilotUserAgent)
	}
	if got := headers.Get("Editor-Version"); got != copilotEditorVersion {
		t.Fatalf("Editor-Version = %q, want %q", got, copilotEditorVersion)
	}
	if got := headers.Get("Editor-Plugin-Version"); got != "copilot-chat/9.9.9" {
		t.Fatalf("Editor-Plugin-Version = %q, want copilot-chat/9.9.9", got)
	}
	if got := headers.Get("Anthropic-Beta"); got != copilotAnthropicBeta {
		t.Fatalf("Anthropic-Beta = %q, want %q", got, copilotAnthropicBeta)
	}
}

func TestHeaderCompiler_PrecedenceMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		computed   string
		authMeta   string
		configured string
		constant   string
		incoming   string
		want       string
	}{
		{
			name:       "computed wins over all",
			computed:   " computed ",
			authMeta:   "auth",
			configured: "config",
			constant:   "constant",
			incoming:   "incoming",
			want:       "computed",
		},
		{
			name:       "auth metadata wins when computed missing",
			computed:   "",
			authMeta:   " auth-meta ",
			configured: "config",
			constant:   "constant",
			incoming:   "incoming",
			want:       "auth-meta",
		},
		{
			name:       "config wins when higher sources missing",
			computed:   "",
			authMeta:   "",
			configured: " config ",
			constant:   "constant",
			incoming:   "incoming",
			want:       "config",
		},
		{
			name:       "constant wins when computed auth and config missing",
			computed:   "",
			authMeta:   "",
			configured: "",
			constant:   " constant ",
			incoming:   "incoming",
			want:       "constant",
		},
		{
			name:       "incoming disabled when all higher sources missing",
			computed:   "",
			authMeta:   "",
			configured: "",
			constant:   "",
			incoming:   "incoming-only",
			want:       "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := resolveHeaderValue(tt.computed, tt.authMeta, tt.configured, tt.constant, tt.incoming)
			if got != tt.want {
				t.Fatalf("resolveHeaderValue(%q, %q, %q, %q, %q) = %q, want %q", tt.computed, tt.authMeta, tt.configured, tt.constant, tt.incoming, got, tt.want)
			}
		})
	}
}

func TestHeaderCompiler_CompileSignatureRequiresStrongTypedContext(t *testing.T) {
	t.Parallel()

	compileMethod, ok := reflect.TypeOf((*githubCopilotHeaderCompiler)(nil)).MethodByName("Compile")
	if !ok {
		t.Fatal("Compile method not found")
	}
	if compileMethod.Type.IsVariadic() {
		t.Fatal("Compile must not be variadic")
	}
	if got, want := compileMethod.Type.NumIn(), 4; got != want {
		t.Fatalf("Compile arg count = %d, want %d", got, want)
	}
	wantContextType := reflect.TypeOf(githubCopilotHeaderCompileContext{})
	if got := compileMethod.Type.In(3); got != wantContextType {
		t.Fatalf("Compile context type = %v, want %v", got, wantContextType)
	}
}

func TestHeaderCompiler_CompileWithAudit_TracksSources(t *testing.T) {
	t.Parallel()

	policy := config.GitHubCopilotHeaderPolicyConfig{
		EditorVersion: "editor-from-config",
	}
	compiler := newGitHubCopilotHeaderCompiler(false, policy)
	resolver := &headerCompilerSessionResolverStub{
		pair: githubCopilotSessionPair{
			SessionID:     "00000000-0000-7000-8000-000000000001",
			InteractionID: "00000000-0000-7000-8000-000000000002",
		},
	}
	compiler.sessionStateManager = resolver

	auth := &cliproxyauth.Auth{Metadata: map[string]any{
		"user_agent": "ua-from-auth",
	}}
	ctx := githubCopilotHeaderCompileContext{
		Body:           []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		Model:          "gpt-4o",
		BucketIdentity: "bucket-a",
	}

	headers, audit := compiler.CompileWithAudit(http.Header{}, auth, ctx)

	if got := headers.Get("User-Agent"); got != "ua-from-auth" {
		t.Fatalf("User-Agent = %q, want ua-from-auth", got)
	}
	if got := audit.HeaderSources["User-Agent"]; got != githubCopilotHeaderSourceAuthMetadata {
		t.Fatalf("User-Agent source = %q, want %q", got, githubCopilotHeaderSourceAuthMetadata)
	}
	if got := audit.HeaderSources["Editor-Version"]; got != githubCopilotHeaderSourceConfig {
		t.Fatalf("Editor-Version source = %q, want %q", got, githubCopilotHeaderSourceConfig)
	}
	if got := audit.HeaderSources["Editor-Plugin-Version"]; got != githubCopilotHeaderSourceConstant {
		t.Fatalf("Editor-Plugin-Version source = %q, want %q", got, githubCopilotHeaderSourceConstant)
	}
	if got := audit.HeaderSources["Anthropic-Beta"]; got != githubCopilotHeaderSourceConstant {
		t.Fatalf("Anthropic-Beta source = %q, want %q", got, githubCopilotHeaderSourceConstant)
	}
	if got := audit.Session.Decision; got != githubCopilotSessionAuditDecisionResolved {
		t.Fatalf("session decision = %q, want %q", got, githubCopilotSessionAuditDecisionResolved)
	}
	if got, want := resolver.calls, 1; got != want {
		t.Fatalf("resolver calls = %d, want %d", got, want)
	}
}

func TestHeaderCompiler_SessionManagerGuard_SkipsWhenModelMissing(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(false)
	resolver := &headerCompilerSessionResolverStub{}
	compiler.sessionStateManager = resolver

	ctx := githubCopilotHeaderCompileContext{
		Body:           []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		Model:          "",
		BucketIdentity: "bucket-a",
	}

	headers, audit := compiler.CompileWithAudit(http.Header{}, nil, ctx)
	if got := resolver.calls; got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := headers.Get("Vscode-Sessionid"); got != "" {
		t.Fatalf("Vscode-Sessionid = %q, want empty", got)
	}
	if got := headers.Get("X-Interaction-Id"); got != "" {
		t.Fatalf("X-Interaction-Id = %q, want empty", got)
	}
	if got := audit.Session.Decision; got != githubCopilotSessionAuditDecisionSkipped {
		t.Fatalf("session decision = %q, want %q", got, githubCopilotSessionAuditDecisionSkipped)
	}
	if got := audit.Session.Reason; got != githubCopilotSessionAuditReasonMissingModel {
		t.Fatalf("session reason = %q, want %q", got, githubCopilotSessionAuditReasonMissingModel)
	}
}

func TestHeaderCompiler_SessionManagerGuard_SkipsWhenBucketMissing(t *testing.T) {
	t.Parallel()

	compiler := newGitHubCopilotHeaderCompiler(false)
	resolver := &headerCompilerSessionResolverStub{}
	compiler.sessionStateManager = resolver

	ctx := githubCopilotHeaderCompileContext{
		Body:  []byte(`{"messages":[{"role":"user","content":"hi"}]}`),
		Model: "gpt-4o",
	}

	headers, audit := compiler.CompileWithAudit(http.Header{}, nil, ctx)
	if got := resolver.calls; got != 0 {
		t.Fatalf("resolver calls = %d, want 0", got)
	}
	if got := headers.Get("Vscode-Sessionid"); got != "" {
		t.Fatalf("Vscode-Sessionid = %q, want empty", got)
	}
	if got := headers.Get("X-Interaction-Id"); got != "" {
		t.Fatalf("X-Interaction-Id = %q, want empty", got)
	}
	if got := audit.Session.Decision; got != githubCopilotSessionAuditDecisionSkipped {
		t.Fatalf("session decision = %q, want %q", got, githubCopilotSessionAuditDecisionSkipped)
	}
	if got := audit.Session.Reason; got != githubCopilotSessionAuditReasonMissingBucket {
		t.Fatalf("session reason = %q, want %q", got, githubCopilotSessionAuditReasonMissingBucket)
	}
}

func TestHeaderCompiler_RequestIDFallback_UsesRandomUUIDWhenV7Fails(t *testing.T) {
	originalV7 := githubCopilotRequestIDNewV7
	originalRandom := githubCopilotRequestIDNewRandom
	originalFallback := githubCopilotRequestIDFallback
	defer func() {
		githubCopilotRequestIDNewV7 = originalV7
		githubCopilotRequestIDNewRandom = originalRandom
		githubCopilotRequestIDFallback = originalFallback
	}()

	githubCopilotRequestIDNewV7 = func() (uuid.UUID, error) {
		return uuid.UUID{}, errors.New("v7 failed")
	}
	fallbackRandom := uuid.MustParse("00000000-0000-4000-8000-000000000042")
	githubCopilotRequestIDNewRandom = func() (uuid.UUID, error) {
		return fallbackRandom, nil
	}
	githubCopilotRequestIDFallback = func() uuid.UUID {
		return uuid.MustParse("00000000-0000-5000-8000-000000000099")
	}

	compiler := newGitHubCopilotHeaderCompiler(false)
	headers, audit := compiler.CompileWithAudit(http.Header{}, nil, githubCopilotHeaderCompileContext{})

	if got := headers.Get("X-Request-Id"); got != fallbackRandom.String() {
		t.Fatalf("X-Request-Id = %q, want %q", got, fallbackRandom.String())
	}
	if got := audit.RequestIDSource; got != githubCopilotRequestIDSourceRandom {
		t.Fatalf("request id source = %q, want %q", got, githubCopilotRequestIDSourceRandom)
	}
}

func TestHeaderCompiler_RequestIDFallback_UsesFinalFallbackWhenAllGeneratorsFail(t *testing.T) {
	originalV7 := githubCopilotRequestIDNewV7
	originalRandom := githubCopilotRequestIDNewRandom
	originalFallback := githubCopilotRequestIDFallback
	defer func() {
		githubCopilotRequestIDNewV7 = originalV7
		githubCopilotRequestIDNewRandom = originalRandom
		githubCopilotRequestIDFallback = originalFallback
	}()

	githubCopilotRequestIDNewV7 = func() (uuid.UUID, error) {
		return uuid.UUID{}, errors.New("v7 failed")
	}
	githubCopilotRequestIDNewRandom = func() (uuid.UUID, error) {
		return uuid.UUID{}, errors.New("random failed")
	}
	fallbackUUID := uuid.MustParse("00000000-0000-5000-8000-000000000123")
	githubCopilotRequestIDFallback = func() uuid.UUID {
		return fallbackUUID
	}

	compiler := newGitHubCopilotHeaderCompiler(false)
	headers, audit := compiler.CompileWithAudit(http.Header{}, nil, githubCopilotHeaderCompileContext{})

	requestID := headers.Get("X-Request-Id")
	if requestID != fallbackUUID.String() {
		t.Fatalf("X-Request-Id = %q, want %q", requestID, fallbackUUID.String())
	}
	if _, err := uuid.Parse(requestID); err != nil {
		t.Fatalf("X-Request-Id parse error: %v", err)
	}
	if got := audit.RequestIDSource; got != githubCopilotRequestIDSourceFallback {
		t.Fatalf("request id source = %q, want %q", got, githubCopilotRequestIDSourceFallback)
	}
}

func TestHeaderCompiler_UserInitiatorGeneratesSessionPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	compiler := newGitHubCopilotHeaderCompiler(false)
	compiler.sessionStateManager = newGitHubCopilotSessionStateManager(stateFile)

	incoming := http.Header{}
	incoming.Set("X-Initiator", "agent")
	incoming.Set("Vscode-Sessionid", "incoming-session")
	incoming.Set("X-Interaction-Id", "incoming-interaction")

	ctx := githubCopilotHeaderCompileContext{
		Body:           []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		Model:          "gpt-4o",
		BucketIdentity: "bucket-user",
	}
	headers := compiler.Compile(incoming, nil, ctx)

	if got := headers.Get("X-Initiator"); got != "user" {
		t.Fatalf("X-Initiator = %q, want user", got)
	}
	pair := assertHeaderCompilerSessionPairPresent(t, headers)

	disk, err := readStateDisk(stateFile)
	if err != nil {
		t.Fatalf("read state disk: %v", err)
	}
	key := githubCopilotSessionStateKey(ctx.Model, ctx.BucketIdentity)
	persisted, ok := disk.Pairs[key]
	if !ok {
		t.Fatalf("expected persisted pair for key %q", key)
	}
	if persisted != pair {
		t.Fatalf("persisted pair mismatch: got %+v want %+v", persisted, pair)
	}
}

func TestHeaderCompiler_AgentInitiatorReusesOrGeneratesSessionPair(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	compiler := newGitHubCopilotHeaderCompiler(false)
	compiler.sessionStateManager = newGitHubCopilotSessionStateManager(stateFile)

	ctx := githubCopilotHeaderCompileContext{
		Body:           []byte(`{"messages":[{"role":"assistant","content":"done"},{"role":"user","content":"continue"}]}`),
		Model:          "gpt-4o",
		BucketIdentity: "bucket-agent",
	}

	headersA := compiler.Compile(http.Header{"X-Initiator": []string{"user"}}, nil, ctx)
	if got := headersA.Get("X-Initiator"); got != "agent" {
		t.Fatalf("first X-Initiator = %q, want agent", got)
	}
	pairA := assertHeaderCompilerSessionPairPresent(t, headersA)

	headersB := compiler.Compile(http.Header{}, nil, ctx)
	if got := headersB.Get("X-Initiator"); got != "agent" {
		t.Fatalf("second X-Initiator = %q, want agent", got)
	}
	pairB := assertHeaderCompilerSessionPairPresent(t, headersB)
	if pairA != pairB {
		t.Fatalf("agent should reuse persisted pair: first %+v second %+v", pairA, pairB)
	}

	disk, err := readStateDisk(stateFile)
	if err != nil {
		t.Fatalf("read state disk: %v", err)
	}
	key := githubCopilotSessionStateKey(ctx.Model, ctx.BucketIdentity)
	persisted, ok := disk.Pairs[key]
	if !ok {
		t.Fatalf("expected persisted pair for key %q", key)
	}
	if persisted != pairA {
		t.Fatalf("persisted pair mismatch: got %+v want %+v", persisted, pairA)
	}
}

func TestHeaderCompiler_PersistFailure_StillEmitsConsistentPairHeaders(t *testing.T) {
	t.Parallel()

	stateFile := filepath.Join(t.TempDir(), "copilot-session-state.json")
	mgr := newGitHubCopilotSessionStateManager(stateFile)
	mgr.rename = func(string, string) error {
		return errors.New("forced rename failure")
	}

	compiler := newGitHubCopilotHeaderCompiler(false)
	compiler.sessionStateManager = mgr

	ctx := githubCopilotHeaderCompileContext{
		Body:           []byte(`{"messages":[{"role":"assistant","content":"done"}]}`),
		Model:          "gpt-4o",
		BucketIdentity: "bucket-persist-failure",
	}

	headersA := compiler.Compile(http.Header{"Vscode-Sessionid": []string{"incoming-only"}}, nil, ctx)
	pairA := assertHeaderCompilerSessionPairPresent(t, headersA)

	headersB := compiler.Compile(http.Header{}, nil, ctx)
	pairB := assertHeaderCompilerSessionPairPresent(t, headersB)

	if pairA == pairB {
		t.Fatalf("expected regenerated pair when persist fails: pairA %+v pairB %+v", pairA, pairB)
	}
}

func assertHeaderCompilerSessionPairPresent(t *testing.T, headers http.Header) githubCopilotSessionPair {
	t.Helper()

	pair := githubCopilotSessionPair{
		SessionID:     headers.Get("Vscode-Sessionid"),
		InteractionID: headers.Get("X-Interaction-Id"),
	}
	assertPairBothOrNeither(t, pair)
	assertPairPresent(t, pair)
	return pair
}

type headerCompilerSessionResolverStub struct {
	calls         int
	lastModel     string
	lastBucket    string
	lastInitiator string
	pair          githubCopilotSessionPair
	err           error
}

func (s *headerCompilerSessionResolverStub) ResolvePair(model, bucketIdentity, initiator string) (githubCopilotSessionPair, error) {
	s.calls++
	s.lastModel = model
	s.lastBucket = bucketIdentity
	s.lastInitiator = initiator
	return s.pair, s.err
}
