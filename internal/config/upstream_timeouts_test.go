package config

import (
	"testing"
)

func TestGetUpstreamTimeouts_Defaults(t *testing.T) {
	// Test with nil config - should return defaults
	connectTimeout, responseHeaderTimeout, err := GetUpstreamTimeouts(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connectTimeout != DefaultConnectTimeoutSeconds {
		t.Errorf("expected connect timeout %d, got %d", DefaultConnectTimeoutSeconds, connectTimeout)
	}
	if responseHeaderTimeout != DefaultResponseHeaderTimeoutSeconds {
		t.Errorf("expected response header timeout %d, got %d", DefaultResponseHeaderTimeoutSeconds, responseHeaderTimeout)
	}
}

func TestGetUpstreamTimeouts_ZeroValues(t *testing.T) {
	// Test with zero values - should return defaults (0 means use Go default)
	cfg := &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        0,
			ResponseHeaderTimeoutSeconds: 0,
		},
	}
	connectTimeout, responseHeaderTimeout, err := GetUpstreamTimeouts(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Zero values should fall back to defaults
	if connectTimeout != DefaultConnectTimeoutSeconds {
		t.Errorf("expected connect timeout %d for zero value, got %d", DefaultConnectTimeoutSeconds, connectTimeout)
	}
	if responseHeaderTimeout != DefaultResponseHeaderTimeoutSeconds {
		t.Errorf("expected response header timeout %d for zero value, got %d", DefaultResponseHeaderTimeoutSeconds, responseHeaderTimeout)
	}
}

func TestGetUpstreamTimeouts_CustomValues(t *testing.T) {
	// Test with custom positive values
	cfg := &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        15,
			ResponseHeaderTimeoutSeconds: 45,
		},
	}
	connectTimeout, responseHeaderTimeout, err := GetUpstreamTimeouts(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connectTimeout != 15 {
		t.Errorf("expected connect timeout 15, got %d", connectTimeout)
	}
	if responseHeaderTimeout != 45 {
		t.Errorf("expected response header timeout 45, got %d", responseHeaderTimeout)
	}
}

func TestGetUpstreamTimeouts_PartialConfig(t *testing.T) {
	// Test with only connect timeout configured
	cfg := &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        20,
			ResponseHeaderTimeoutSeconds: 0, // Use default
		},
	}
	connectTimeout, responseHeaderTimeout, err := GetUpstreamTimeouts(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connectTimeout != 20 {
		t.Errorf("expected connect timeout 20, got %d", connectTimeout)
	}
	if responseHeaderTimeout != DefaultResponseHeaderTimeoutSeconds {
		t.Errorf("expected response header timeout %d, got %d", DefaultResponseHeaderTimeoutSeconds, responseHeaderTimeout)
	}

	// Test with only response header timeout configured
	cfg = &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        0, // Use default
			ResponseHeaderTimeoutSeconds: 60,
		},
	}
	connectTimeout, responseHeaderTimeout, err = GetUpstreamTimeouts(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if connectTimeout != DefaultConnectTimeoutSeconds {
		t.Errorf("expected connect timeout %d, got %d", DefaultConnectTimeoutSeconds, connectTimeout)
	}
	if responseHeaderTimeout != 60 {
		t.Errorf("expected response header timeout 60, got %d", responseHeaderTimeout)
	}
}

func TestGetUpstreamTimeouts_NegativeConnectTimeout(t *testing.T) {
	// Test with negative connect timeout - should return error
	cfg := &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        -5,
			ResponseHeaderTimeoutSeconds: 30,
		},
	}
	_, _, err := GetUpstreamTimeouts(cfg)
	if err == nil {
		t.Fatal("expected error for negative connect timeout, got nil")
	}
	invalidErr, ok := err.(*InvalidTimeoutError)
	if !ok {
		t.Fatalf("expected InvalidTimeoutError, got %T", err)
	}
	if invalidErr.Field != "connect-timeout-seconds" {
		t.Errorf("expected field 'connect-timeout-seconds', got '%s'", invalidErr.Field)
	}
	if invalidErr.Value != -5 {
		t.Errorf("expected value -5, got %d", invalidErr.Value)
	}
}

func TestGetUpstreamTimeouts_NegativeResponseHeaderTimeout(t *testing.T) {
	// Test with negative response header timeout - should return error
	cfg := &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        10,
			ResponseHeaderTimeoutSeconds: -10,
		},
	}
	_, _, err := GetUpstreamTimeouts(cfg)
	if err == nil {
		t.Fatal("expected error for negative response header timeout, got nil")
	}
	invalidErr, ok := err.(*InvalidTimeoutError)
	if !ok {
		t.Fatalf("expected InvalidTimeoutError, got %T", err)
	}
	if invalidErr.Field != "response-header-timeout-seconds" {
		t.Errorf("expected field 'response-header-timeout-seconds', got '%s'", invalidErr.Field)
	}
	if invalidErr.Value != -10 {
		t.Errorf("expected value -10, got %d", invalidErr.Value)
	}
}

func TestGetUpstreamTimeouts_BothNegative(t *testing.T) {
	// Test with both negative - should return error for connect (checked first)
	cfg := &SDKConfig{
		UpstreamTimeouts: UpstreamTimeouts{
			ConnectTimeoutSeconds:        -1,
			ResponseHeaderTimeoutSeconds: -2,
		},
	}
	_, _, err := GetUpstreamTimeouts(cfg)
	if err == nil {
		t.Fatal("expected error for negative timeouts, got nil")
	}
	invalidErr, ok := err.(*InvalidTimeoutError)
	if !ok {
		t.Fatalf("expected InvalidTimeoutError, got %T", err)
	}
	// Connect timeout is checked first
	if invalidErr.Field != "connect-timeout-seconds" {
		t.Errorf("expected field 'connect-timeout-seconds', got '%s'", invalidErr.Field)
	}
}

func TestInvalidTimeoutError_Error(t *testing.T) {
	err := &InvalidTimeoutError{
		Field: "test-field",
		Value: -42,
	}
	expected := "invalid timeout value for test-field: negative values are not allowed"
	if err.Error() != expected {
		t.Errorf("expected error message '%s', got '%s'", expected, err.Error())
	}
}
