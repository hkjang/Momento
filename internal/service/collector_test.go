package service

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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
	got := sanitizeURL("https://example.com/a?keep=1&token=secret#access_token=private", cfg)
	if got != "https://example.com/a?keep=1&token=%5BMASKED%5D" {
		t.Fatalf("got %s", got)
	}
	cfg.StripQueryString = true
	if got := sanitizeURL("https://example.com/a?x=1#employee", cfg); got != "https://example.com/a" {
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

func TestStrictestValidationMode(t *testing.T) {
	tests := []struct {
		a, b, want string
	}{
		{"allow", "allow", "allow"},
		{"allow", "warn", "warn"},
		{"warn", "allow", "warn"},
		{"warn", "reject", "reject"},
		{"reject", "allow", "reject"},
	}
	for _, tt := range tests {
		if got := strictestValidationMode(tt.a, tt.b); got != tt.want {
			t.Errorf("strictestValidationMode(%q,%q)=%q want %q", tt.a, tt.b, got, tt.want)
		}
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

func TestProtectPIIBeforeDurableQueue(t *testing.T) {
	req := model.CollectRequest{
		UserProperties:    map[string]any{"contact": "user@example.com"},
		SessionProperties: map[string]any{"operator_phone": "010-2222-3333"},
		Context:           model.EventContext{Page: model.PageContext{Title: "010-1234-5678 고객"}},
		Events: []model.IncomingEvent{{Name: "error", Properties: map[string]any{
			"message": "Authorization: Bearer secret-token-value-12345",
			"card":    "4111 1111 1111 1111",
		}}},
	}
	count, kinds := protectPII(&req, "mask")
	if count != 5 || len(kinds) != 4 {
		t.Fatalf("unexpected detection count=%d kinds=%v", count, kinds)
	}
	if req.UserProperties["contact"] != "[MASKED_EMAIL]" || req.Context.Page.Title != "[MASKED_PHONE] 고객" {
		t.Fatalf("PII was not masked: %#v", req)
	}
	if req.SessionProperties["operator_phone"] != "[MASKED_PHONE]" {
		t.Fatalf("session PII was not masked: %#v", req.SessionProperties)
	}
	if strings.Contains(fmt.Sprint(req.Events[0].Properties), "secret-token") || strings.Contains(fmt.Sprint(req.Events[0].Properties), "4111") {
		t.Fatalf("credential or card survived masking: %#v", req.Events[0].Properties)
	}
}

func TestActiveEngagementMilliseconds(t *testing.T) {
	tests := []struct {
		event model.IncomingEvent
		want  int64
	}{
		{event: model.IncomingEvent{Name: "click", Properties: map[string]any{"active_seconds": 15.0}}, want: 0},
		{event: model.IncomingEvent{Name: "user_engagement", Properties: map[string]any{"active_seconds": 12.5}}, want: 12500},
		{event: model.IncomingEvent{Name: "user_engagement", Properties: map[string]any{"active_seconds": "7"}}, want: 7000},
		{event: model.IncomingEvent{Name: "user_engagement", Properties: map[string]any{"active_seconds": 99999.0}}, want: 3600000},
		{event: model.IncomingEvent{Name: "user_engagement", Properties: map[string]any{"active_seconds": -1.0}}, want: 0},
	}
	for _, tt := range tests {
		if got := activeEngagementMilliseconds(tt.event); got != tt.want {
			t.Errorf("activeEngagementMilliseconds(%#v)=%d want %d", tt.event, got, tt.want)
		}
	}
}

func TestInteractionEvents(t *testing.T) {
	for _, name := range []string{"click", "search", "form_submit", "conversion", "purchase", "error"} {
		if !isInteractionEvent(name) {
			t.Errorf("expected %s to be an interaction", name)
		}
	}
	for _, name := range []string{"page_view", "user_engagement", "scroll"} {
		if isInteractionEvent(name) {
			t.Errorf("did not expect %s to be an interaction", name)
		}
	}
}

func TestEventRevenue(t *testing.T) {
	tests := []struct {
		event model.IncomingEvent
		want  float64
	}{
		{event: model.IncomingEvent{Name: "purchase", Properties: map[string]any{"value": 12500.5}}, want: 12500.5},
		{event: model.IncomingEvent{Name: "purchase", Properties: map[string]any{"revenue": "3300"}}, want: 3300},
		{event: model.IncomingEvent{Name: "refund", Properties: map[string]any{"value": 100.0}}, want: 0},
		{event: model.IncomingEvent{Name: "purchase", Properties: map[string]any{"value": "invalid"}}, want: 0},
	}
	for _, tt := range tests {
		if got := eventRevenue(tt.event); got != tt.want {
			t.Errorf("eventRevenue(%#v)=%v want %v", tt.event, got, tt.want)
		}
	}
}

// TestAutomaticEventsCoverEverythingTheTrackerEmits reads the tracker source so
// that adding an automatic event without exempting it from contract
// registration fails here instead of in a customer's reject-mode environment,
// where one unregistered event drops the whole batch.
func TestAutomaticEventsCoverEverythingTheTrackerEmits(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "sdk", "src", "index.ts"))
	if err != nil {
		t.Skipf("tracker source is not available: %v", err)
	}
	emitted := regexp.MustCompile(`(?:track|signal)\("([a-z_]+)"`).FindAllStringSubmatch(string(source), -1)
	if len(emitted) < 10 {
		t.Fatalf("found only %d emitted events, the pattern no longer matches the tracker", len(emitted))
	}
	seen := map[string]bool{}
	for _, match := range emitted {
		name := match[1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if !automaticEvents[name] {
			t.Errorf("the tracker emits %q but it is not in automaticEvents, so a reject-mode environment would drop the batch", name)
		}
	}
}

var _ = model.CollectRequest{}
