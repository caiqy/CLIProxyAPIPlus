package cliproxy

import (
	"reflect"
	"slices"
	"testing"
)

func TestGetStaticProviderModelUniverse_NormalizesAndSorts(t *testing.T) {
	models := GetStaticProviderModelUniverse("  ANTIGRAVITY  ")
	if len(models) == 0 {
		t.Fatal("expected non-empty antigravity static models")
	}
	if !slices.IsSorted(models) {
		t.Fatalf("models should be sorted: %v", models)
	}
	if !slices.Contains(models, "gemini-2.5-flash") {
		t.Fatalf("expected known static model in universe, got %v", models)
	}
}

func TestGetStaticProviderModelUniverse_OpenAIAliasToCodex(t *testing.T) {
	openaiModels := GetStaticProviderModelUniverse("openai-compatibility")
	codexModels := GetStaticProviderModelUniverse("codex")
	if len(openaiModels) == 0 || len(codexModels) == 0 {
		t.Fatal("expected non-empty codex/openai alias static models")
	}
	if !reflect.DeepEqual(openaiModels, codexModels) {
		t.Fatalf("openai alias mismatch: openai=%v codex=%v", openaiModels, codexModels)
	}
}
