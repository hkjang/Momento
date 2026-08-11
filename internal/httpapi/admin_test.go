package httpapi

import (
	"strings"
	"testing"
)

func TestTrackingSnippetContainsSafeSDKConfiguration(t *testing.T) {
	t.Parallel()

	got := trackingSnippet("https://momento.example/", "SITE_123", "stg", "consent-required")
	for _, want := range []string{
		`src="https://momento.example/tracker.js"`,
		`data-site-id="SITE_123"`,
		`data-environment="stg"`,
		`data-mode="consent-required"`,
		`data-auto-rum="true"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("snippet %q does not contain %q", got, want)
		}
	}
}

func TestTrackingSnippetEscapesAttributes(t *testing.T) {
	t.Parallel()

	got := trackingSnippet(`https://example.com/\"><script>`, `SITE_\"`, "prd", "full")
	if strings.Contains(got, `<script><script>`) || strings.Contains(got, `SITE_\"`) {
		t.Fatalf("snippet contains an unescaped attribute: %q", got)
	}
}

func TestValidateAdminSettingPIIDetectionMode(t *testing.T) {
	for _, mode := range []string{"detect", "warn", "mask", "reject"} {
		t.Run(mode, func(t *testing.T) {
			if err := validateAdminSetting("privacy", map[string]any{"pii_detection_mode": mode}); err != nil {
				t.Fatalf("valid mode %q was rejected: %v", mode, err)
			}
		})
	}

	for _, value := range []any{"off", true, 1.0} {
		if err := validateAdminSetting("privacy", map[string]any{"pii_detection_mode": value}); err == nil {
			t.Fatalf("invalid mode %#v was accepted", value)
		}
	}
}

func TestCSPGuidanceNamesTheCollectorOrigin(t *testing.T) {
	t.Parallel()

	guidance := cspGuidance("https://momento.kubagents-ofc.koreacb.com/")
	if guidance["collector_origin"] != "https://momento.kubagents-ofc.koreacb.com" {
		t.Fatalf("collector_origin = %q", guidance["collector_origin"])
	}
	if !strings.Contains(guidance["header"], "connect-src 'self' https://momento.kubagents-ofc.koreacb.com") {
		t.Fatalf("header = %q, want the collector origin in connect-src", guidance["header"])
	}
	if !strings.Contains(guidance["header"], "script-src 'self' https://momento.kubagents-ofc.koreacb.com") {
		t.Fatalf("header = %q, want the collector origin in script-src", guidance["header"])
	}
	if !strings.Contains(guidance["meta"], "Content-Security-Policy") {
		t.Fatalf("meta = %q", guidance["meta"])
	}
	if !strings.Contains(guidance["proxy_snippet"], "proxy_pass https://momento.kubagents-ofc.koreacb.com/") {
		t.Fatalf("proxy_snippet = %q", guidance["proxy_snippet"])
	}
}

func TestValidateAdminSettingConnectOrigins(t *testing.T) {
	t.Parallel()

	if err := validateAdminSetting("security", map[string]any{"additional_connect_origins": []any{"https://momento.example", "collector.internal"}}); err != nil {
		t.Fatalf("valid origins were rejected: %v", err)
	}
	for _, value := range []any{[]any{"javascript:alert(1)"}, []any{1.0}, "https://momento.example"} {
		if err := validateAdminSetting("security", map[string]any{"additional_connect_origins": value}); err == nil {
			t.Fatalf("invalid value %#v was accepted", value)
		}
	}
}

func TestKeyStorageMessageExplainsRecoverability(t *testing.T) {
	t.Parallel()

	if !strings.Contains(keyStorageMessage(true), "shown again") {
		t.Fatalf("recoverable message = %q", keyStorageMessage(true))
	}
	if !strings.Contains(keyStorageMessage(false), "MOMENTO_ENCRYPTION_KEY") {
		t.Fatalf("non recoverable message = %q", keyStorageMessage(false))
	}
}

func TestUnlistedOriginsRespectsWildcards(t *testing.T) {
	t.Parallel()

	observed := []string{"intranet.corp.local", "portal.corp.local", "shadow.example"}
	got := unlistedOrigins(observed, []string{"*.corp.local"})
	if len(got) != 1 || got[0] != "shadow.example" {
		t.Fatalf("unlisted = %v, want [shadow.example]", got)
	}
	if len(unlistedOrigins(observed, nil)) != 0 {
		t.Fatal("an empty allowlist allows every origin")
	}
	if len(unlistedOrigins([]string{"corp.local"}, []string{"*.corp.local"})) != 0 {
		t.Fatal("a wildcard should also cover the apex host")
	}
}
