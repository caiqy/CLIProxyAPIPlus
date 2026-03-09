package executor

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func resetKiroHTTPClientPoolsForTest() {
	kiroHTTPClientPoolMu.Lock()
	defer kiroHTTPClientPoolMu.Unlock()
	kiroHTTPClientPools = make(map[string]*http.Client)
}

func TestGetKiroPooledHTTPClient_AppliesInsecureSkipVerify(t *testing.T) {
	resetKiroHTTPClientPoolsForTest()

	client := getKiroPooledHTTPClient(&config.Config{SDKConfig: config.SDKConfig{TLSInsecureSkipVerify: true}})
	transport, ok := client.Transport.(*http.Transport)
	if !ok || transport == nil {
		t.Fatalf("expected *http.Transport, got %T", client.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected InsecureSkipVerify=true, got %#v", transport.TLSClientConfig)
	}
}

func TestGetKiroPooledHTTPClient_PoolKeyIncludesInsecureSwitch(t *testing.T) {
	resetKiroHTTPClientPoolsForTest()

	insecureClient := getKiroPooledHTTPClient(&config.Config{SDKConfig: config.SDKConfig{TLSInsecureSkipVerify: true}})
	insecureTransport, _ := insecureClient.Transport.(*http.Transport)
	if insecureTransport == nil || insecureTransport.TLSClientConfig == nil || !insecureTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected insecure transport, got %#v", insecureTransport)
	}

	strictClient := getKiroPooledHTTPClient(&config.Config{SDKConfig: config.SDKConfig{TLSInsecureSkipVerify: false}})
	strictTransport, _ := strictClient.Transport.(*http.Transport)
	if strictTransport == nil || strictTransport.TLSClientConfig == nil || strictTransport.TLSClientConfig.InsecureSkipVerify {
		t.Fatalf("expected strict transport, got %#v", strictTransport)
	}
}
