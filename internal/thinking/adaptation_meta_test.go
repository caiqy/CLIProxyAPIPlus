package thinking_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/thinking"
	_ "github.com/router-for-me/CLIProxyAPI/v6/internal/thinking/provider/openai"
	"github.com/tidwall/gjson"
)

func registerAdaptationMetaTestModels(t *testing.T) {
	t.Helper()
	reg := registry.GetGlobalRegistry()
	uid := fmt.Sprintf("adaptation-meta-%d", time.Now().UnixNano())
	reg.RegisterClient(uid, "test", []*registry.ModelInfo{
		{
			ID:       "meta-supported-model",
			Thinking: &registry.ThinkingSupport{Levels: []string{"none", "low", "medium", "high", "xhigh"}, ZeroAllowed: true, DynamicAllowed: false},
		},
		{
			ID:       "meta-subset-model",
			Thinking: &registry.ThinkingSupport{Levels: []string{"low", "high"}, ZeroAllowed: false, DynamicAllowed: false},
		},
		{
			ID:          "meta-user-defined-model",
			UserDefined: true,
			Thinking:    nil,
		},
	})
	t.Cleanup(func() {
		reg.UnregisterClient(uid)
	})
}

func TestApplyThinkingWithMeta_NoExplicitVariant(t *testing.T) {
	registerAdaptationMetaTestModels(t)
	body := []byte(`{"model":"meta-supported-model","messages":[{"role":"user","content":"hi"}]}`)

	out, meta, err := thinking.ApplyThinkingWithMeta(body, "meta-supported-model", "openai", "openai", "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.VariantOrigin != "" || meta.Variant != "" {
		t.Fatalf("expected empty variants, got origin=%q variant=%q", meta.VariantOrigin, meta.Variant)
	}
	if got := gjson.GetBytes(out, "reasoning_effort"); got.Exists() {
		t.Fatalf("expected no reasoning_effort for no explicit config, got %s", got.Raw)
	}
}

func TestApplyThinkingWithMeta_PreserveSupportedXHigh(t *testing.T) {
	registerAdaptationMetaTestModels(t)
	body := []byte(`{"model":"meta-supported-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`)

	out, meta, err := thinking.ApplyThinkingWithMeta(body, "meta-supported-model", "openai", "openai", "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.VariantOrigin != "xhigh" || meta.Variant != "xhigh" {
		t.Fatalf("expected origin=variant=xhigh, got origin=%q variant=%q", meta.VariantOrigin, meta.Variant)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "xhigh" {
		t.Fatalf("expected reasoning_effort=xhigh, got %q", got)
	}
}

func TestApplyThinkingWithMeta_DowngradeUnsupportedXHigh(t *testing.T) {
	registerAdaptationMetaTestModels(t)
	body := []byte(`{"model":"meta-subset-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"xhigh"}`)

	out, meta, err := thinking.ApplyThinkingWithMeta(body, "meta-subset-model", "gemini", "openai", "openai")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if meta.VariantOrigin != "xhigh" || meta.Variant != "high" {
		t.Fatalf("expected origin=xhigh and variant=high, got origin=%q variant=%q", meta.VariantOrigin, meta.Variant)
	}
	if got := gjson.GetBytes(out, "reasoning_effort").String(); got != "high" {
		t.Fatalf("expected reasoning_effort=high, got %q", got)
	}
}

func TestApplyThinkingWithMeta_UserDefinedUnknownLevelReturnsError(t *testing.T) {
	registerAdaptationMetaTestModels(t)
	body := []byte(`{"model":"meta-user-defined-model","messages":[{"role":"user","content":"hi"}],"reasoning_effort":"ultra"}`)

	_, _, err := thinking.ApplyThinkingWithMeta(body, "meta-user-defined-model", "openai", "openai", "openai")
	if err == nil {
		t.Fatal("expected unknown level error for user-defined model")
	}
}
