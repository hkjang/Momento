package httpapi

import "testing"

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
