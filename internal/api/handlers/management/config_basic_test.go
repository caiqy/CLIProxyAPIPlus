package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestGetConfigReturnsCurrentConfigSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &Handler{cfg: &config.Config{Debug: true, CommercialMode: true, LoggingToFile: true}}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/config", nil)

	h.GetConfig(c)

	if w.Code != http.StatusOK {
		t.Fatalf("unexpected status code: got %d want %d", w.Code, http.StatusOK)
	}

	var got config.Config
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !got.Debug {
		t.Fatalf("expected debug=true in response")
	}
	if !got.CommercialMode {
		t.Fatalf("expected commercial-mode=true in response")
	}
	if !got.LoggingToFile {
		t.Fatalf("expected logging-to-file=true in response")
	}
}
