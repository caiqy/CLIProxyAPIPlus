package management

import (
	"context"
	"net/http"
	"testing"

	geminiAuth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/gemini"
)

func TestShouldVerifyCloudAPIForProjectSelection(t *testing.T) {
	if shouldVerifyCloudAPIForProjectSelection("") {
		t.Fatalf("empty project_id should skip cloud api verification")
	}

	if !shouldVerifyCloudAPIForProjectSelection("my-project") {
		t.Fatalf("explicit project_id should verify cloud api")
	}
}

func TestEnsureGeminiProjectAndOnboard_EmptyProject_UsesDiscovery(t *testing.T) {
	originalSetup := performGeminiCLISetupFn
	performGeminiCLISetupFn = func(ctx context.Context, httpClient *http.Client, storage *geminiAuth.GeminiTokenStorage, requestedProject string) error {
		if requestedProject != "" {
			t.Fatalf("expected empty requestedProject for discovery, got %q", requestedProject)
		}
		storage.ProjectID = "discovered-project"
		return nil
	}
	t.Cleanup(func() {
		performGeminiCLISetupFn = originalSetup
	})

	storage := &geminiAuth.GeminiTokenStorage{}
	err := ensureGeminiProjectAndOnboard(context.Background(), &http.Client{}, storage, "")
	if err != nil {
		t.Fatalf("ensureGeminiProjectAndOnboard returned error: %v", err)
	}

	if !storage.Auto {
		t.Fatalf("expected storage.Auto=true for empty project request")
	}

	if storage.ProjectID != "discovered-project" {
		t.Fatalf("expected discovered project id, got %q", storage.ProjectID)
	}
}

func TestGeminiOnboardingFailureMessage(t *testing.T) {
	if got := geminiOnboardingFailureMessage("", nil); got != "Failed to auto-discover project ID" {
		t.Fatalf("unexpected message for empty project: %q", got)
	}

	if got := geminiOnboardingFailureMessage("my-project", nil); got != "Failed to complete Gemini CLI onboarding" {
		t.Fatalf("unexpected message for explicit project: %q", got)
	}
}
