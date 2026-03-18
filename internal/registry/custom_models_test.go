package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOverlayModels_ReplacesByIDKeepsPosition(t *testing.T) {
	base := &staticModelsJSON{
		Claude: []*ModelInfo{
			{ID: "a", OwnedBy: "base-a"},
			{ID: "b", OwnedBy: "base-b"},
			{ID: "c", OwnedBy: "base-c"},
		},
	}
	custom := &staticModelsJSON{
		Claude: []*ModelInfo{{ID: "b", OwnedBy: "custom-b", DisplayName: "B custom"}},
	}

	merged, summaries := overlayModels(base, custom)
	if merged == nil {
		t.Fatal("merged should not be nil")
	}
	if got := len(merged.Claude); got != 3 {
		t.Fatalf("merged.Claude len = %d, want 3", got)
	}
	gotIDs := []string{merged.Claude[0].ID, merged.Claude[1].ID, merged.Claude[2].ID}
	wantIDs := []string{"a", "b", "c"}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("merged ids = %v, want %v", gotIDs, wantIDs)
		}
	}
	if got := merged.Claude[1].OwnedBy; got != "custom-b" {
		t.Fatalf("merged.Claude[1].OwnedBy = %q, want %q", got, "custom-b")
	}
	if got := merged.Claude[1].DisplayName; got != "B custom" {
		t.Fatalf("merged.Claude[1].DisplayName = %q, want %q", got, "B custom")
	}
	if summaries["claude"].Overridden != 1 || summaries["claude"].Added != 0 {
		t.Fatalf("claude summary = %+v, want overridden=1 added=0", summaries["claude"])
	}
	if base.Claude[1].OwnedBy != "base-b" {
		t.Fatal("overlay should not mutate base slice entries")
	}
}

func TestOverlayModels_AppendsNewIDsToTail(t *testing.T) {
	base := &staticModelsJSON{
		Claude: []*ModelInfo{{ID: "a"}, {ID: "b"}},
	}
	custom := &staticModelsJSON{
		Claude: []*ModelInfo{{ID: "c", OwnedBy: "custom-c"}},
	}

	merged, summaries := overlayModels(base, custom)
	if merged == nil {
		t.Fatal("merged should not be nil")
	}
	if got := len(merged.Claude); got != 3 {
		t.Fatalf("merged.Claude len = %d, want 3", got)
	}
	if got := merged.Claude[2].ID; got != "c" {
		t.Fatalf("tail id = %q, want %q", got, "c")
	}
	if got := merged.Claude[2].OwnedBy; got != "custom-c" {
		t.Fatalf("tail owned_by = %q, want %q", got, "custom-c")
	}
	if summaries["claude"].Overridden != 0 || summaries["claude"].Added != 1 {
		t.Fatalf("claude summary = %+v, want overridden=0 added=1", summaries["claude"])
	}
}

func TestOverlayModels_AddsWholeProviderSectionWhenBaseMissing(t *testing.T) {
	base := &staticModelsJSON{}
	custom := &staticModelsJSON{
		Qwen: []*ModelInfo{{ID: "qwen-foo", OwnedBy: "custom"}},
	}

	merged, summaries := overlayModels(base, custom)
	if merged == nil {
		t.Fatal("merged should not be nil")
	}
	if got := len(merged.Qwen); got != 1 {
		t.Fatalf("merged.Qwen len = %d, want 1", got)
	}
	if got := merged.Qwen[0].ID; got != "qwen-foo" {
		t.Fatalf("merged.Qwen[0].ID = %q, want %q", got, "qwen-foo")
	}
	if summaries["qwen"].Added != 1 || summaries["qwen"].Overridden != 0 {
		t.Fatalf("qwen summary = %+v, want overridden=0 added=1", summaries["qwen"])
	}
}

func TestParseCustomModels_IgnoresUnknownProviderAndBadItems(t *testing.T) {
	path := writeTempCustomModelsFile(t, `{
		"claude": [
			{"id": "ok-1", "owned_by": "custom"},
			{"id": "   ", "owned_by": "bad"},
			null
		],
		"unknown-provider": [{"id": "ignored"}]
	}`)

	parsed, err := parseCustomModelsFile(path)
	if err != nil {
		t.Fatalf("parseCustomModelsFile() error = %v", err)
	}
	if parsed == nil {
		t.Fatal("parsed should not be nil")
	}
	if got := len(parsed.Claude); got != 1 {
		t.Fatalf("parsed.Claude len = %d, want 1", got)
	}
	if parsed.Claude[0].ID != "ok-1" {
		t.Fatalf("parsed.Claude[0].ID = %q, want %q", parsed.Claude[0].ID, "ok-1")
	}
	if len(parsed.Qwen) != 0 {
		t.Fatalf("parsed.Qwen len = %d, want 0", len(parsed.Qwen))
	}
}

func TestParseCustomModels_AllowsPartialSections(t *testing.T) {
	path := writeTempCustomModelsFile(t, `{
		"claude": [{"id": "claude-custom"}]
	}`)

	parsed, err := parseCustomModelsFile(path)
	if err != nil {
		t.Fatalf("parseCustomModelsFile() error = %v", err)
	}
	if parsed == nil {
		t.Fatal("parsed should not be nil")
	}
	if got := len(parsed.Claude); got != 1 {
		t.Fatalf("parsed.Claude len = %d, want 1", got)
	}
	if got := len(parsed.Gemini); got != 0 {
		t.Fatalf("parsed.Gemini len = %d, want 0", got)
	}
}

func TestParseCustomModels_FileNotFoundFallsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-custom-models.json")

	parsed, err := parseCustomModelsFile(path)
	if err != nil {
		t.Fatalf("parseCustomModelsFile() error = %v, want nil", err)
	}
	if parsed != nil {
		t.Fatalf("parsed = %#v, want nil", parsed)
	}
}

func TestParseCustomModels_InvalidJSONFallsBack(t *testing.T) {
	path := writeTempCustomModelsFile(t, `{"claude": [}`)

	parsed, err := parseCustomModelsFile(path)
	if err != nil {
		t.Fatalf("parseCustomModelsFile() error = %v, want nil", err)
	}
	if parsed != nil {
		t.Fatalf("parsed = %#v, want nil", parsed)
	}
}

func TestParseCustomModels_SectionTypeMismatchIgnored(t *testing.T) {
	path := writeTempCustomModelsFile(t, `{
		"claude": {"id": "wrong-type"},
		"gemini": [{"id": "gemini-ok"}]
	}`)

	parsed, err := parseCustomModelsFile(path)
	if err != nil {
		t.Fatalf("parseCustomModelsFile() error = %v", err)
	}
	if parsed == nil {
		t.Fatal("parsed should not be nil")
	}
	if got := len(parsed.Claude); got != 0 {
		t.Fatalf("parsed.Claude len = %d, want 0", got)
	}
	if got := len(parsed.Gemini); got != 1 {
		t.Fatalf("parsed.Gemini len = %d, want 1", got)
	}
}

func writeTempCustomModelsFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom_models.json")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
