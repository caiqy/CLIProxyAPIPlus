package executor

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestIsTimeoutError_Nil(t *testing.T) {
	timeoutType, isTimeout := IsTimeoutError(nil)
	if isTimeout {
		t.Error("expected false for nil error")
	}
	if timeoutType != "" {
		t.Errorf("expected empty timeout type, got %s", timeoutType)
	}
}

func TestIsTimeoutError_ContextDeadlineExceeded(t *testing.T) {
	timeoutType, isTimeout := IsTimeoutError(context.DeadlineExceeded)
	if !isTimeout {
		t.Error("expected true for context.DeadlineExceeded")
	}
	if timeoutType != TimeoutTypeUnknown {
		t.Errorf("expected timeout type %s, got %s", TimeoutTypeUnknown, timeoutType)
	}
}

// mockNetError implements net.Error for testing
type mockNetError struct {
	timeout   bool
	temporary bool
	message   string
}

func (e *mockNetError) Error() string   { return e.message }
func (e *mockNetError) Timeout() bool   { return e.timeout }
func (e *mockNetError) Temporary() bool { return e.temporary }

func TestIsTimeoutError_NetErrorTimeout(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantTimeout bool
		wantType    string
	}{
		{
			name:        "dial timeout",
			err:         &mockNetError{timeout: true, message: "dial tcp: i/o timeout"},
			wantTimeout: true,
			wantType:    TimeoutTypeConnect,
		},
		{
			name:        "connect timeout",
			err:         &mockNetError{timeout: true, message: "connect: connection timed out"},
			wantTimeout: true,
			wantType:    TimeoutTypeConnect,
		},
		{
			name:        "response header timeout",
			err:         &mockNetError{timeout: true, message: "net/http: timeout awaiting response headers"},
			wantTimeout: true,
			wantType:    TimeoutTypeResponseHeader,
		},
		{
			name:        "generic timeout",
			err:         &mockNetError{timeout: true, message: "operation timed out"},
			wantTimeout: true,
			wantType:    TimeoutTypeUnknown,
		},
		{
			name:        "non-timeout net error",
			err:         &mockNetError{timeout: false, message: "connection refused"},
			wantTimeout: false,
			wantType:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotTimeout := IsTimeoutError(tt.err)
			if gotTimeout != tt.wantTimeout {
				t.Errorf("IsTimeoutError() timeout = %v, want %v", gotTimeout, tt.wantTimeout)
			}
			if gotType != tt.wantType {
				t.Errorf("IsTimeoutError() type = %v, want %v", gotType, tt.wantType)
			}
		})
	}
}

func TestIsTimeoutError_RegularError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		wantTimeout bool
		wantType    string
	}{
		{
			name:        "timeout in message - dial",
			err:         errors.New("dial tcp timeout"),
			wantTimeout: true,
			wantType:    TimeoutTypeConnect,
		},
		{
			name:        "timeout in message - connect",
			err:         errors.New("connect timeout exceeded"),
			wantTimeout: true,
			wantType:    TimeoutTypeConnect,
		},
		{
			name:        "timeout in message - response header",
			err:         errors.New("timeout awaiting response headers"),
			wantTimeout: true,
			wantType:    TimeoutTypeResponseHeader,
		},
		{
			name:        "timeout in message - generic",
			err:         errors.New("operation timeout"),
			wantTimeout: true,
			wantType:    TimeoutTypeUnknown,
		},
		{
			name:        "no timeout",
			err:         errors.New("connection refused"),
			wantTimeout: false,
			wantType:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotTimeout := IsTimeoutError(tt.err)
			if gotTimeout != tt.wantTimeout {
				t.Errorf("IsTimeoutError() timeout = %v, want %v", gotTimeout, tt.wantTimeout)
			}
			if gotType != tt.wantType {
				t.Errorf("IsTimeoutError() type = %v, want %v", gotType, tt.wantType)
			}
		})
	}
}

func TestBuildDefaultTransportWithTimeouts(t *testing.T) {
	tests := []struct {
		name                      string
		connectTimeoutSec         int
		responseHeaderTimeoutSec  int
		wantResponseHeaderTimeout time.Duration
	}{
		{
			name:                      "both positive",
			connectTimeoutSec:         10,
			responseHeaderTimeoutSec:  30,
			wantResponseHeaderTimeout: 30 * time.Second,
		},
		{
			name:                      "zero values",
			connectTimeoutSec:         0,
			responseHeaderTimeoutSec:  0,
			wantResponseHeaderTimeout: 0, // Go default
		},
		{
			name:                      "custom values",
			connectTimeoutSec:         15,
			responseHeaderTimeoutSec:  45,
			wantResponseHeaderTimeout: 45 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := buildDefaultTransportWithTimeouts(tt.connectTimeoutSec, tt.responseHeaderTimeoutSec)
			if transport == nil {
				t.Fatal("expected non-nil transport")
			}
			if transport.ResponseHeaderTimeout != tt.wantResponseHeaderTimeout {
				t.Errorf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, tt.wantResponseHeaderTimeout)
			}
			// Verify DialContext is set when connect timeout > 0
			if tt.connectTimeoutSec > 0 && transport.DialContext == nil {
				t.Error("expected DialContext to be set for positive connect timeout")
			}
		})
	}
}

func TestBuildProxyTransport_InvalidURL(t *testing.T) {
	transport := buildProxyTransport("", 10, 30)
	if transport != nil {
		t.Error("expected nil transport for empty URL")
	}

	transport = buildProxyTransport("://invalid", 10, 30)
	if transport != nil {
		t.Error("expected nil transport for invalid URL")
	}
}

func TestBuildProxyTransport_UnsupportedScheme(t *testing.T) {
	transport := buildProxyTransport("ftp://proxy.example.com:8080", 10, 30)
	if transport != nil {
		t.Error("expected nil transport for unsupported scheme")
	}
}

func TestBuildProxyTransport_HTTPProxy(t *testing.T) {
	transport := buildProxyTransport("http://proxy.example.com:8080", 10, 30)
	if transport == nil {
		t.Fatal("expected non-nil transport for HTTP proxy")
	}
	if transport.Proxy == nil {
		t.Error("expected Proxy to be set for HTTP proxy")
	}
	if transport.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 30*time.Second)
	}
}

func TestBuildProxyTransport_HTTPSProxy(t *testing.T) {
	transport := buildProxyTransport("https://proxy.example.com:8080", 15, 45)
	if transport == nil {
		t.Fatal("expected non-nil transport for HTTPS proxy")
	}
	if transport.Proxy == nil {
		t.Error("expected Proxy to be set for HTTPS proxy")
	}
	if transport.ResponseHeaderTimeout != 45*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 45*time.Second)
	}
}

func TestBuildProxyTransport_SOCKS5Proxy(t *testing.T) {
	// Note: This test only verifies the transport is created, not that SOCKS5 actually works
	transport := buildProxyTransport("socks5://proxy.example.com:1080", 10, 30)
	if transport == nil {
		t.Fatal("expected non-nil transport for SOCKS5 proxy")
	}
	if transport.DialContext == nil {
		t.Error("expected DialContext to be set for SOCKS5 proxy")
	}
	if transport.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", transport.ResponseHeaderTimeout, 30*time.Second)
	}
}

func TestBuildProxyTransport_SOCKS5WithAuth(t *testing.T) {
	transport := buildProxyTransport("socks5://user:pass@proxy.example.com:1080", 10, 30)
	if transport == nil {
		t.Fatal("expected non-nil transport for SOCKS5 proxy with auth")
	}
	if transport.DialContext == nil {
		t.Error("expected DialContext to be set for SOCKS5 proxy")
	}
}

func TestBuildProxyTransport_ZeroTimeouts(t *testing.T) {
	// Test with zero timeouts - should use fallback for SOCKS5
	transport := buildProxyTransport("socks5://proxy.example.com:1080", 0, 0)
	if transport == nil {
		t.Fatal("expected non-nil transport")
	}
	// ResponseHeaderTimeout should be 0 (Go default) when configured as 0
	if transport.ResponseHeaderTimeout != 0 {
		t.Errorf("ResponseHeaderTimeout = %v, want 0", transport.ResponseHeaderTimeout)
	}
}

// TestDialContextTimeout verifies that the custom DialContext respects timeout
func TestDialContextTimeout(t *testing.T) {
	transport := buildDefaultTransportWithTimeouts(1, 30) // 1 second connect timeout
	if transport == nil || transport.DialContext == nil {
		t.Fatal("expected transport with DialContext")
	}

	// Try to dial a non-routable IP address to trigger timeout
	ctx := context.Background()
	start := time.Now()
	_, err := transport.DialContext(ctx, "tcp", "10.255.255.1:80")
	elapsed := time.Since(start)

	// Should timeout within reasonable time (1s + some buffer)
	if err == nil {
		t.Error("expected error for non-routable address")
	}
	// Check that it didn't take too long (should be around 1 second)
	if elapsed > 5*time.Second {
		t.Errorf("dial took too long: %v (expected ~1s)", elapsed)
	}

	// Verify it's a timeout error
	var netErr net.Error
	if errors.As(err, &netErr) && !netErr.Timeout() {
		// Some systems may return different errors, so we just log
		t.Logf("dial error (may not be timeout on all systems): %v", err)
	}
}
