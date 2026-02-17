package management

import (
	"testing"
	"time"
)

func TestNewOAuthSessionStore_DefaultTTL(t *testing.T) {
	store := newOAuthSessionStore(0)
	if store.ttl != 30*time.Minute {
		t.Fatalf("expected default oauth session ttl to be 30m, got %v", store.ttl)
	}
}
