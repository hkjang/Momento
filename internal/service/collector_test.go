package service

import (
	"net"
	"testing"

	"github.com/hkjang/Momento/internal/model"
)

func TestOriginAllowed(t *testing.T) {
	tests := []struct {
		origin  string
		domains []string
		want    bool
	}{{"https://app.example.com", []string{"app.example.com"}, true}, {"https://sub.example.com", []string{"*.example.com"}, true}, {"https://evil-example.com", []string{"*.example.com"}, false}, {"", nil, true}, {"", []string{"example.com"}, false}}
	for _, tt := range tests {
		if got := originAllowed(tt.origin, tt.domains); got != tt.want {
			t.Errorf("originAllowed(%q)=%v want %v", tt.origin, got, tt.want)
		}
	}
}
func TestAnonymizeIP(t *testing.T) {
	if got := anonymizeIP(net.ParseIP("192.168.10.42")).String(); got != "192.168.10.0" {
		t.Fatalf("got %s", got)
	}
	if got := anonymizeIP(net.ParseIP("2001:db8:1:2:3:4:5:6")).String(); got != "2001:db8:1:2::" {
		t.Fatalf("got %s", got)
	}
}
func TestSanitizeURL(t *testing.T) {
	cfg := privacyConfig{MaskedParameters: []string{"token"}}
	got := sanitizeURL("https://example.com/a?keep=1&token=secret", cfg)
	if got != "https://example.com/a?keep=1&token=%5BMASKED%5D" {
		t.Fatalf("got %s", got)
	}
	cfg.StripQueryString = true
	if got := sanitizeURL("https://example.com/a?x=1", cfg); got != "https://example.com/a" {
		t.Fatalf("got %s", got)
	}
}
func TestFilteredProperties(t *testing.T) {
	got := filteredProperties(map[string]any{"email": "secret", "feature": "search", "nested": map[string]any{"phone": "010", "safe": true}}, []string{"EMAIL", "phone"})
	if _, ok := got["email"]; ok {
		t.Fatal("blocked key survived")
	}
	if got["feature"] != "search" {
		t.Fatal("safe key was removed")
	}
	if _, ok := got["nested"].(map[string]any)["phone"]; ok {
		t.Fatal("nested blocked key survived")
	}
}
func TestValidateProperties(t *testing.T) {
	schema := []byte(`{"required":["feature"],"properties":{"feature":{"type":"string"},"price":{"type":"number"}}}`)
	if warnings := validateProperties(map[string]any{"feature": "search", "price": 42.0}, schema); len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	if warnings := validateProperties(map[string]any{"price": "expensive"}, schema); len(warnings) != 2 {
		t.Fatalf("expected two warnings: %v", warnings)
	}
}
func TestPrivacyAppliedBeforeDurableQueue(t *testing.T) {
	req := model.CollectRequest{UserID: "EMP001", UserProperties: map[string]any{"email": "secret", "department": "Platform"}, Context: model.EventContext{Page: model.PageContext{URL: "https://example.test/a?token=secret"}}, Events: []model.IncomingEvent{{Name: "click", Properties: map[string]any{"phone": "010", "feature": "search"}}}}
	ip, ua := "192.168.1.42", "Browser/1"
	applyPrivacyBeforeQueue(&req, &ip, &ua, privacyConfig{IPAnonymization: true, CollectUserAgent: false, CollectUserID: false, MaskedParameters: []string{"token"}, BlockedProperties: []string{"email", "phone"}})
	if req.UserID != "" || ua != "" || ip != "192.168.1.0" {
		t.Fatalf("identifier privacy failed: %#v %q %q", req, ip, ua)
	}
	if req.Context.Page.URL != "https://example.test/a?token=%5BMASKED%5D" {
		t.Fatalf("url was not masked: %s", req.Context.Page.URL)
	}
	if _, ok := req.UserProperties["email"]; ok {
		t.Fatal("PII persisted before queue")
	}
	if _, ok := req.Events[0].Properties["phone"]; ok {
		t.Fatal("event PII persisted before queue")
	}
}

var _ = model.CollectRequest{}
