package registry

import (
	"reflect"
	"testing"
)

func TestGetModelProviders_FallsBackToClientRegistrationsWhenProviderIndexMissing(t *testing.T) {
	r := newTestModelRegistry()
	r.RegisterClient("client-1", "github-copilot", []*ModelInfo{{ID: "claude-sonnet-4-6"}})

	registration := r.models["claude-sonnet-4-6"]
	if registration == nil {
		t.Fatal("expected model registration to exist")
	}
	registration.Providers = nil

	got := r.GetModelProviders("claude-sonnet-4-6")
	want := []string{"github-copilot"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetModelProviders() = %v, want %v", got, want)
	}
}
