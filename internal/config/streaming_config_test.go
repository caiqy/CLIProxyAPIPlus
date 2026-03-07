package config

import "testing"

func TestStreamingConfig_AnthropicSSELifecycleEnabled_DefaultsToTrue(t *testing.T) {
	var cfg StreamingConfig
	if !cfg.AnthropicSSELifecycleEnabled() {
		t.Fatal("expected anthropic SSE lifecycle normalizer to default to enabled")
	}
}

func TestStreamingConfig_AnthropicSSELifecycleEnabled_ExplicitFalseDisables(t *testing.T) {
	disabled := false
	cfg := StreamingConfig{AnthropicSSELifecycleEnable: &disabled}
	if cfg.AnthropicSSELifecycleEnabled() {
		t.Fatal("expected explicit false to disable anthropic SSE lifecycle normalizer")
	}
}

func TestStreamingConfig_AnthropicSSELifecycleEnabled_ExplicitTrueEnables(t *testing.T) {
	enabled := true
	cfg := StreamingConfig{AnthropicSSELifecycleEnable: &enabled}
	if !cfg.AnthropicSSELifecycleEnabled() {
		t.Fatal("expected explicit true to enable anthropic SSE lifecycle normalizer")
	}
}
