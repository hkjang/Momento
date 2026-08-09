package service

import "testing"

func TestValidateDeliveryEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		allowed  []string
		wantErr  bool
	}{
		{"exact host", "https://hooks.internal.example/v1", []string{"hooks.internal.example"}, false},
		{"wildcard subdomain", "https://team.notify.example/v1", []string{"*.notify.example"}, false},
		{"wildcard excludes apex", "https://notify.example/v1", []string{"*.notify.example"}, true},
		{"host suffix confusion", "https://notify.example.attacker.test/v1", []string{"*.notify.example"}, true},
		{"embedded credentials", "https://token@hooks.internal.example/v1", []string{"hooks.internal.example"}, true},
		{"unsupported scheme", "file:///etc/passwd", []string{"hooks.internal.example"}, true},
		{"empty allowlist", "https://hooks.internal.example/v1", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDeliveryEndpoint(tt.endpoint, tt.allowed)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateDeliveryEndpoint() error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}
