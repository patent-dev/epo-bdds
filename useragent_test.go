package bdds

import (
	"context"
	"net/http"
	"testing"
)

// TestDefaultUserAgent verifies that leaving Config.UserAgent empty falls back to
// DefaultUserAgent and that value is set on outbound requests.
func TestDefaultUserAgent(t *testing.T) {
	client, err := NewClient(&Config{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.config.UserAgent != DefaultUserAgent {
		t.Errorf("config.UserAgent = %q, want %q", client.config.UserAgent, DefaultUserAgent)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.org", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := client.authRequestEditor(req.Context(), req); err != nil {
		t.Fatalf("authRequestEditor: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != DefaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got, DefaultUserAgent)
	}
}

// TestCustomUserAgent verifies that a caller-supplied Config.UserAgent overrides
// the default.
func TestCustomUserAgent(t *testing.T) {
	client, err := NewClient(&Config{UserAgent: "Custom/9.9"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://example.org", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if err := client.authRequestEditor(req.Context(), req); err != nil {
		t.Fatalf("authRequestEditor: %v", err)
	}
	if got := req.Header.Get("User-Agent"); got != "Custom/9.9" {
		t.Errorf("User-Agent = %q, want %q", got, "Custom/9.9")
	}
}
