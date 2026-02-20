package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	sdktranslator "github.com/router-for-me/CLIProxyAPI/v6/sdk/translator"
)

type captureUsagePlugin struct {
	recordCh chan usage.Record
}

func (p *captureUsagePlugin) HandleUsage(_ context.Context, record usage.Record) {
	select {
	case p.recordCh <- record:
	default:
	}
}

func TestUsageReporterPublishesVariantFields(t *testing.T) {
	pl := &captureUsagePlugin{recordCh: make(chan usage.Record, 8)}
	usage.RegisterPlugin(pl)

	reporter := newUsageReporter(context.Background(), "variant-provider", "variant-model", nil)
	reporter.setThinkingVariant("xhigh", "high")
	reporter.publish(context.Background(), usage.Detail{InputTokens: 1, TotalTokens: 1})

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case rec := <-pl.recordCh:
			if rec.Provider != "variant-provider" || rec.Model != "variant-model" {
				continue
			}
			if rec.VariantOrigin != "xhigh" || rec.Variant != "high" {
				t.Fatalf("unexpected variants: origin=%q variant=%q", rec.VariantOrigin, rec.Variant)
			}
			return
		case <-timer.C:
			t.Fatal("timed out waiting for usage record")
		}
	}
}

func TestAIStudioExecuteFailurePublishesVariantFromThinkingMeta(t *testing.T) {
	pl := &captureUsagePlugin{recordCh: make(chan usage.Record, 8)}
	usage.RegisterPlugin(pl)

	exec := NewAIStudioExecutor(nil, "aistudio", nil)
	_, err := exec.Execute(context.Background(), nil, cliproxyexecutor.Request{
		Model:   "gpt-5(xhigh)",
		Payload: []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err == nil {
		t.Fatal("expected execute error for unsupported level")
	}

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case rec := <-pl.recordCh:
			if rec.Provider != "aistudio" || rec.Model != "gpt-5" {
				continue
			}
			if !rec.Failed {
				continue
			}
			if rec.VariantOrigin != "xhigh" || rec.Variant != "" {
				t.Fatalf("expected failed record variant origin xhigh and empty variant, got %q => %q", rec.VariantOrigin, rec.Variant)
			}
			return
		case <-timer.C:
			t.Fatal("timed out waiting for failed usage record")
		}
	}
}

func TestApplyThinkingWithUsageMeta_AllProviderPathsCaptureVariantOrigin(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		from        string
		to          string
		providerKey string
	}{
		{name: "openai-provider", from: "openai", to: "openai", providerKey: "openai"},
		{name: "openai-compat", from: "openai", to: "openai", providerKey: "openrouter"},
		{name: "qwen", from: "openai", to: "openai", providerKey: "qwen"},
		{name: "kilo", from: "openai", to: "openai", providerKey: "kilo"},
		{name: "codex", from: "openai", to: "codex", providerKey: "codex"},
		{name: "github-copilot-responses", from: "claude", to: "codex", providerKey: "github-copilot"},
		{name: "claude", from: "openai", to: "claude", providerKey: "claude"},
		{name: "gemini", from: "openai", to: "gemini", providerKey: "gemini"},
		{name: "aistudio", from: "openai", to: "gemini", providerKey: "aistudio"},
		{name: "vertex", from: "openai", to: "gemini", providerKey: "vertex"},
		{name: "gemini-cli", from: "openai", to: "gemini-cli", providerKey: "gemini-cli"},
		{name: "antigravity", from: "openai", to: "antigravity", providerKey: "antigravity"},
		{name: "iflow", from: "openai", to: "iflow", providerKey: "iflow"},
		{name: "kimi", from: "openai", to: "kimi", providerKey: "kimi"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reporter := newUsageReporter(context.Background(), tc.providerKey, "gpt-5", nil)
			_, err := applyThinkingWithUsageMeta(
				[]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
				"gpt-5(xhigh)",
				tc.from,
				tc.to,
				tc.providerKey,
				reporter,
			)
			if reporter.variantOrigin != "xhigh" {
				t.Fatalf("expected variantOrigin xhigh, got %q", reporter.variantOrigin)
			}
			if err != nil && strings.TrimSpace(reporter.variant) != "" {
				t.Fatalf("expected empty variant when validation fails, got %q (err=%v)", reporter.variant, err)
			}
			if err == nil && strings.TrimSpace(reporter.variant) == "" {
				t.Fatalf("expected non-empty variant when adaptation succeeds, got empty")
			}
		})
	}
}

func TestTranslateKiroRequestWithThinkingMeta_CapturesVariantOriginForOpenAISource(t *testing.T) {
	t.Parallel()

	reporter := newUsageReporter(context.Background(), "kiro", "gpt-5", nil)
	_, err := translateKiroRequestWithThinkingMeta(
		[]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
		"gpt-5(xhigh)",
		sdktranslator.FromString("openai"),
		reporter,
	)
	_ = err
	if reporter.variantOrigin != "xhigh" || reporter.variant != "" {
		t.Fatalf("expected variant origin xhigh and empty variant, got %q => %q", reporter.variantOrigin, reporter.variant)
	}
}

func TestTranslateKiroRequestWithThinkingMeta_CapturesVariantOriginForClaudeSource(t *testing.T) {
	t.Parallel()

	reporter := newUsageReporter(context.Background(), "kiro", "gpt-5", nil)
	_, err := translateKiroRequestWithThinkingMeta(
		[]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
		"gpt-5(xhigh)",
		sdktranslator.FromString("claude"),
		reporter,
	)
	_ = err
	if reporter.variantOrigin != "xhigh" || reporter.variant != "" {
		t.Fatalf("expected variant origin xhigh and empty variant, got %q => %q", reporter.variantOrigin, reporter.variant)
	}
}

func TestKiroExecuteFailurePublishesVariantFromThinkingMeta(t *testing.T) {
	pl := &captureUsagePlugin{recordCh: make(chan usage.Record, 8)}
	usage.RegisterPlugin(pl)

	exec := NewKiroExecutor(nil)
	auth := &cliproxyauth.Auth{
		Provider: "kiro",
		Metadata: map[string]any{"access_token": "abc"},
		Attributes: map[string]string{
			"access_token": "abc",
		},
	}
	_, err := exec.Execute(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5(xhigh)",
		Payload: []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err == nil {
		t.Fatal("expected execute error")
	}

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case rec := <-pl.recordCh:
			if rec.Provider != "kiro" || !rec.Failed {
				continue
			}
			if rec.VariantOrigin != "xhigh" || rec.Variant != "" {
				t.Fatalf("expected failed record variant origin xhigh and empty variant, got %q => %q", rec.VariantOrigin, rec.Variant)
			}
			return
		case <-timer.C:
			t.Fatal("timed out waiting for failed kiro usage record")
		}
	}
}

func TestKiroExecuteStreamFailurePublishesVariantFromThinkingMeta(t *testing.T) {
	pl := &captureUsagePlugin{recordCh: make(chan usage.Record, 8)}
	usage.RegisterPlugin(pl)

	exec := NewKiroExecutor(nil)
	auth := &cliproxyauth.Auth{
		Provider: "kiro",
		Metadata: map[string]any{"access_token": "abc"},
		Attributes: map[string]string{
			"access_token": "abc",
		},
	}
	_, err := exec.ExecuteStream(context.Background(), auth, cliproxyexecutor.Request{
		Model:   "gpt-5(xhigh)",
		Payload: []byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
	}, cliproxyexecutor.Options{SourceFormat: sdktranslator.FromString("openai")})
	if err == nil {
		t.Fatal("expected execute stream error")
	}

	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case rec := <-pl.recordCh:
			if rec.Provider != "kiro" || !rec.Failed {
				continue
			}
			if rec.VariantOrigin != "xhigh" || rec.Variant != "" {
				t.Fatalf("expected failed record variant origin xhigh and empty variant, got %q => %q", rec.VariantOrigin, rec.Variant)
			}
			return
		case <-timer.C:
			t.Fatal("timed out waiting for failed kiro stream usage record")
		}
	}
}

func TestKiroWebSearchFollowupWithNilReporter_DoesNotOverwriteCapturedVariant(t *testing.T) {
	t.Parallel()

	reporter := newUsageReporter(context.Background(), "kiro", "gpt-5(xhigh)", nil)
	_, err := translateKiroRequestWithThinkingMeta(
		[]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
		"gpt-5(xhigh)",
		sdktranslator.FromString("openai"),
		reporter,
	)
	_ = err
	if reporter.variantOrigin != "xhigh" {
		t.Fatalf("expected initial variantOrigin to be captured, got %q", reporter.variantOrigin)
	}

	// Simulate web_search follow-up GAR call where reporter must be nil to avoid
	// clearing previously captured variant metadata.
	_, err = translateKiroRequestWithThinkingMeta(
		[]byte(`{"model":"gpt-5","messages":[{"role":"user","content":"hi"}]}`),
		"gpt-5",
		sdktranslator.FromString("openai"),
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error in follow-up translation: %v", err)
	}
	if reporter.variantOrigin != "xhigh" {
		t.Fatalf("expected variantOrigin to remain xhigh, got %q", reporter.variantOrigin)
	}
}
