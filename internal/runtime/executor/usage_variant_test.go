package executor

import (
	"context"
	"testing"
	"time"

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
