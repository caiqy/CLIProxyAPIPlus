package executor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
	"golang.org/x/net/proxy"
)

// Timeout type constants for logging
const (
	TimeoutTypeConnect        = "connect"
	TimeoutTypeResponseHeader = "response_header"
	TimeoutTypeUnknown        = "unknown"
)

// httpClientCache caches HTTP clients by proxy URL to enable connection reuse
var (
	httpClientCache      = make(map[string]*http.Client)
	httpClientCacheMutex sync.RWMutex
)

// newProxyAwareHTTPClient creates an HTTP client with proper proxy configuration priority:
// 1. Use auth.ProxyURL if configured (highest priority)
// 2. Use cfg.ProxyURL if auth proxy is not configured
// 3. Use RoundTripper from context if neither are configured
//
// This function caches HTTP clients by proxy URL to enable TCP/TLS connection reuse.
// It also applies upstream timeout configuration from cfg.UpstreamTimeouts.
//
// Parameters:
//   - ctx: The context containing optional RoundTripper
//   - cfg: The application configuration (includes upstream timeout settings)
//   - auth: The authentication information
//   - timeout: The client timeout (0 means no timeout)
//
// Returns:
//   - *http.Client: An HTTP client with configured proxy or transport
func newProxyAwareHTTPClient(ctx context.Context, cfg *config.Config, auth *cliproxyauth.Auth, timeout time.Duration) *http.Client {
	// Priority 1: Use auth.ProxyURL if configured
	var proxyURL string
	if auth != nil {
		proxyURL = strings.TrimSpace(auth.ProxyURL)
	}

	// Priority 2: Use cfg.ProxyURL if auth proxy is not configured
	if proxyURL == "" && cfg != nil {
		proxyURL = strings.TrimSpace(cfg.ProxyURL)
	}

	// Get upstream timeout configuration
	var sdkCfg *config.SDKConfig
	if cfg != nil {
		sdkCfg = &cfg.SDKConfig
	}
	connectTimeout, responseHeaderTimeout, err := config.GetUpstreamTimeouts(sdkCfg)
	if err != nil {
		// Negative timeout values are invalid - log error and use defaults
		log.Errorf("invalid upstream timeout configuration: %v, using defaults", err)
		connectTimeout = config.DefaultConnectTimeoutSeconds
		responseHeaderTimeout = config.DefaultResponseHeaderTimeoutSeconds
	}

	// Log timeout configuration at debug level
	log.Debugf("upstream timeouts: connect=%ds, response-header=%ds", connectTimeout, responseHeaderTimeout)

	// Build cache key from proxy URL and timeout config (empty string for no proxy)
	// Include timeout values in cache key to ensure different timeout configs get different clients
	cacheKey := fmt.Sprintf("%s|%d|%d", proxyURL, connectTimeout, responseHeaderTimeout)

	// Check cache first
	httpClientCacheMutex.RLock()
	if cachedClient, ok := httpClientCache[cacheKey]; ok {
		httpClientCacheMutex.RUnlock()
		// Return a wrapper with the requested timeout but shared transport
		if timeout > 0 {
			return &http.Client{
				Transport: cachedClient.Transport,
				Timeout:   timeout,
			}
		}
		return cachedClient
	}
	httpClientCacheMutex.RUnlock()

	// Create new client
	httpClient := &http.Client{}
	if timeout > 0 {
		httpClient.Timeout = timeout
	}

	// If we have a proxy URL configured, set up the transport
	if proxyURL != "" {
		transport := buildProxyTransport(proxyURL, connectTimeout, responseHeaderTimeout)
		if transport != nil {
			httpClient.Transport = transport
			// Cache the client
			httpClientCacheMutex.Lock()
			httpClientCache[cacheKey] = httpClient
			httpClientCacheMutex.Unlock()
			return httpClient
		}
		// If proxy setup failed, log and fall through to context RoundTripper
		log.Debugf("failed to setup proxy from URL: %s, falling back to context transport", proxyURL)
	}

	// No proxy - create transport based on DefaultTransport with timeout configuration
	transport := buildDefaultTransportWithTimeouts(connectTimeout, responseHeaderTimeout)
	httpClient.Transport = transport

	// Priority 3: Use RoundTripper from context (typically from RoundTripperFor)
	// Only if no proxy is configured and context has a custom RoundTripper
	if proxyURL == "" {
		if rt, ok := ctx.Value("cliproxy.roundtripper").(http.RoundTripper); ok && rt != nil {
			httpClient.Transport = rt
		}
	}

	// Cache the client
	httpClientCacheMutex.Lock()
	httpClientCache[cacheKey] = httpClient
	httpClientCacheMutex.Unlock()

	return httpClient
}

// buildDefaultTransportWithTimeouts creates an HTTP transport based on http.DefaultTransport
// with the specified timeout configuration applied.
//
// Parameters:
//   - connectTimeoutSec: TCP connection timeout in seconds (0 means use Go default)
//   - responseHeaderTimeoutSec: Response header timeout in seconds (0 means use Go default)
//
// Returns:
//   - *http.Transport: A configured transport with timeouts applied
func buildDefaultTransportWithTimeouts(connectTimeoutSec, responseHeaderTimeoutSec int) *http.Transport {
	// Clone DefaultTransport to preserve reasonable defaults (MaxIdleConns, TLSHandshakeTimeout, etc.)
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Apply connect timeout via custom Dialer
	if connectTimeoutSec > 0 {
		connectTimeout := time.Duration(connectTimeoutSec) * time.Second
		transport.DialContext = (&net.Dialer{
			Timeout:   connectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
		log.Debugf("applied connect timeout: %v", connectTimeout)
	} else if connectTimeoutSec == 0 {
		log.Warnf("connect-timeout-seconds is 0, using Go default (no explicit timeout)")
	}

	// Apply response header timeout
	if responseHeaderTimeoutSec > 0 {
		responseHeaderTimeout := time.Duration(responseHeaderTimeoutSec) * time.Second
		transport.ResponseHeaderTimeout = responseHeaderTimeout
		log.Debugf("applied response-header timeout: %v", responseHeaderTimeout)
	} else if responseHeaderTimeoutSec == 0 {
		log.Warnf("response-header-timeout-seconds is 0, using Go default (no explicit timeout)")
	}

	return transport
}

// buildProxyTransport creates an HTTP transport configured for the given proxy URL.
// It supports SOCKS5, HTTP, and HTTPS proxy protocols.
// The transport is based on http.DefaultTransport.Clone() to preserve reasonable defaults.
//
// Parameters:
//   - proxyURL: The proxy URL string (e.g., "socks5://user:pass@host:port", "http://host:port")
//   - connectTimeoutSec: TCP connection timeout in seconds (0 means use Go default)
//   - responseHeaderTimeoutSec: Response header timeout in seconds (0 means use Go default)
//
// Returns:
//   - *http.Transport: A configured transport, or nil if the proxy URL is invalid
func buildProxyTransport(proxyURL string, connectTimeoutSec, responseHeaderTimeoutSec int) *http.Transport {
	if proxyURL == "" {
		return nil
	}

	parsedURL, errParse := url.Parse(proxyURL)
	if errParse != nil {
		log.Errorf("parse proxy URL failed: %v", errParse)
		return nil
	}

	// Start with DefaultTransport clone to preserve reasonable defaults
	transport := http.DefaultTransport.(*http.Transport).Clone()

	// Apply response header timeout
	if responseHeaderTimeoutSec > 0 {
		transport.ResponseHeaderTimeout = time.Duration(responseHeaderTimeoutSec) * time.Second
		log.Debugf("proxy transport: applied response-header timeout: %ds", responseHeaderTimeoutSec)
	} else if responseHeaderTimeoutSec == 0 {
		log.Warnf("proxy transport: response-header-timeout-seconds is 0, using Go default")
	}

	// Handle different proxy schemes
	if parsedURL.Scheme == "socks5" {
		// Configure SOCKS5 proxy with optional authentication
		var proxyAuth *proxy.Auth
		if parsedURL.User != nil {
			username := parsedURL.User.Username()
			password, _ := parsedURL.User.Password()
			proxyAuth = &proxy.Auth{User: username, Password: password}
		}
		dialer, errSOCKS5 := proxy.SOCKS5("tcp", parsedURL.Host, proxyAuth, proxy.Direct)
		if errSOCKS5 != nil {
			log.Errorf("create SOCKS5 dialer failed: %v", errSOCKS5)
			return nil
		}

		// Create connect timeout for SOCKS5
		connectTimeout := time.Duration(connectTimeoutSec) * time.Second
		if connectTimeoutSec == 0 {
			connectTimeout = 30 * time.Second // Fallback default for SOCKS5
			log.Warnf("proxy transport (SOCKS5): connect-timeout-seconds is 0, using fallback 30s")
		}

		// Set up a custom DialContext using the SOCKS5 dialer with timeout
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Create a context with timeout for the dial operation
			dialCtx, cancel := context.WithTimeout(ctx, connectTimeout)
			defer cancel()

			// Use a channel to handle the dial result
			type dialResult struct {
				conn net.Conn
				err  error
			}
			resultCh := make(chan dialResult, 1)

			go func() {
				conn, err := dialer.Dial(network, addr)
				resultCh <- dialResult{conn: conn, err: err}
			}()

			select {
			case <-dialCtx.Done():
				return nil, dialCtx.Err()
			case result := <-resultCh:
				return result.conn, result.err
			}
		}
		log.Debugf("proxy transport (SOCKS5): applied connect timeout: %v", connectTimeout)

	} else if parsedURL.Scheme == "http" || parsedURL.Scheme == "https" {
		// Configure HTTP or HTTPS proxy
		transport.Proxy = http.ProxyURL(parsedURL)

		// Apply connect timeout via custom Dialer for HTTP/HTTPS proxy
		if connectTimeoutSec > 0 {
			connectTimeout := time.Duration(connectTimeoutSec) * time.Second
			transport.DialContext = (&net.Dialer{
				Timeout:   connectTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext
			log.Debugf("proxy transport (HTTP/HTTPS): applied connect timeout: %v", connectTimeout)
		} else if connectTimeoutSec == 0 {
			log.Warnf("proxy transport (HTTP/HTTPS): connect-timeout-seconds is 0, using Go default")
		}
	} else {
		log.Errorf("unsupported proxy scheme: %s", parsedURL.Scheme)
		return nil
	}

	return transport
}

// IsTimeoutError checks if an error is a timeout error and returns the timeout type.
// Returns the timeout type (connect, response_header, or unknown) and true if it's a timeout error.
func IsTimeoutError(err error) (timeoutType string, isTimeout bool) {
	if err == nil {
		return "", false
	}

	// Check for context deadline exceeded
	if errors.Is(err, context.DeadlineExceeded) {
		return TimeoutTypeUnknown, true
	}

	// Check for net.Error timeout
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		// Try to determine the type based on error message
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "dial") || strings.Contains(errMsg, "connect") {
			return TimeoutTypeConnect, true
		}
		if strings.Contains(errMsg, "response header") || strings.Contains(errMsg, "awaiting response") {
			return TimeoutTypeResponseHeader, true
		}
		return TimeoutTypeUnknown, true
	}

	// Check error message for timeout patterns
	errMsg := strings.ToLower(err.Error())
	if strings.Contains(errMsg, "timeout") {
		if strings.Contains(errMsg, "dial") || strings.Contains(errMsg, "connect") {
			return TimeoutTypeConnect, true
		}
		if strings.Contains(errMsg, "response header") || strings.Contains(errMsg, "awaiting response") {
			return TimeoutTypeResponseHeader, true
		}
		return TimeoutTypeUnknown, true
	}

	return "", false
}

// LogTimeoutEvent logs a timeout event with relevant context at WARN level.
// This provides observability for timeout events to aid debugging and monitoring.
//
// Parameters:
//   - requestID: The request ID for correlation
//   - provider: The provider name (e.g., "codex", "claude", "kiro")
//   - authID: The auth ID being used
//   - timeoutType: The type of timeout (connect, response_header, unknown)
//   - elapsed: The elapsed time before timeout
//   - err: The original error
func LogTimeoutEvent(requestID, provider, authID, timeoutType string, elapsed time.Duration, err error) {
	log.WithFields(log.Fields{
		"request_id":   requestID,
		"provider":     provider,
		"auth_id":      authID,
		"timeout_type": timeoutType,
		"elapsed_time": elapsed.String(),
	}).Warnf("upstream request timeout: %v", err)
}

// LogTimeoutRetrySuccess logs when a request succeeds after a timeout retry at DEBUG level.
//
// Parameters:
//   - requestID: The request ID for correlation
//   - provider: The provider name
//   - authID: The auth ID being used
//   - attempt: The retry attempt number that succeeded
func LogTimeoutRetrySuccess(requestID, provider, authID string, attempt int) {
	log.WithFields(log.Fields{
		"request_id": requestID,
		"provider":   provider,
		"auth_id":    authID,
		"attempt":    attempt,
	}).Debugf("request succeeded after timeout retry")
}
