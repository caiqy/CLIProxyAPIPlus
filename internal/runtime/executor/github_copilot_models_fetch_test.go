package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestFetchGitHubCopilotModels_DynamicMissingClaude_MergesStaticFallbackModels(t *testing.T) {
	// NOTE: This test mutates http.DefaultTransport; do NOT run in parallel.

	originalTransport := http.DefaultTransport
	t.Cleanup(func() {
		http.DefaultTransport = originalTransport
	})

	http.DefaultTransport = testRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch r.URL.String() {
		case "https://api.github.com/copilot_internal/v2/token":
			// NOTE: expires_at is not used by FetchGitHubCopilotModels; any positive integer is fine.
			body := `{"token":"copilot_api_token","expires_at":9999999999,"endpoints":{"api":"https://api.business.githubcopilot.com"}}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil

		case "https://api.business.githubcopilot.com/models":
			// Dynamic list intentionally omits Claude 4.6 models.
			body := `{"object":"list","data":[{"id":"gemini-3.1-pro-preview","object":"model","created":1770000000,"owned_by":"github-copilot","name":"Gemini 3.1 Pro"}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		default:
			t.Fatalf("unexpected request URL: %s", r.URL.String())
			return nil, nil
		}
	})

	cfg := &config.Config{}
	auth := &cliproxyauth.Auth{Metadata: map[string]any{"access_token": "gho_test_token"}}

	models := FetchGitHubCopilotModels(context.Background(), auth, cfg)
	ids := make(map[string]struct{}, len(models))
	for _, m := range models {
		if m == nil || m.ID == "" {
			continue
		}
		ids[m.ID] = struct{}{}
	}

	if _, ok := ids["gemini-3.1-pro-preview"]; !ok {
		t.Fatalf("expected dynamic model %q to be present", "gemini-3.1-pro-preview")
	}
	if _, ok := ids["claude-sonnet-4.6"]; !ok {
		t.Fatalf("expected static Claude model %q to be present even when missing from dynamic list", "claude-sonnet-4.6")
	}
}
