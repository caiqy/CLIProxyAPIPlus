package util

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestSetProxy_AppliesInsecureSkipVerify(t *testing.T) {
	cfg := &config.SDKConfig{
		ProxyURL:              "http://127.0.0.1:7890",
		TLSInsecureSkipVerify: true,
	}

	client := SetProxy(cfg, &http.Client{})
	transport, _ := client.Transport.(*http.Transport)
	if transport == nil {
		t.Fatal("expected *http.Transport")
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify=true, got %#v", transport.TLSClientConfig)
	}
}

func TestSetProxy_AppliesInsecureSkipVerifyWithoutProxyURL(t *testing.T) {
	cfg := &config.SDKConfig{TLSInsecureSkipVerify: true}

	client := SetProxy(cfg, &http.Client{})
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected *http.Transport when insecure skip verify is enabled, got %T", client.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify=true, got %#v", transport.TLSClientConfig)
	}
}
