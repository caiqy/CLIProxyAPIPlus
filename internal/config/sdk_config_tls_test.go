package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestSDKConfig_TLSInsecureSkipVerifyYAML(t *testing.T) {
	raw := []byte("tls-insecure-skip-verify: true\n")
	var cfg SDKConfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal yaml failed: %v", err)
	}
	if !cfg.TLSInsecureSkipVerify {
		t.Fatal("expected TLSInsecureSkipVerify=true")
	}
}
