package config

import "testing"

func withRequiredEnvironment(t *testing.T) {
	t.Setenv("MOMENTO_POSTGRES_DSN", "postgres://momento@localhost:5432/momento")
	t.Setenv("MOMENTO_BOOTSTRAP_ADMIN", "admin@example.com")
	t.Setenv("MOMENTO_BOOTSTRAP_ADMIN_PASSWORD", "a-long-enough-password")
}

func TestEncryptionKeyAcceptsTheBareVariableName(t *testing.T) {
	withRequiredEnvironment(t)
	t.Setenv("MOMENTO_ENCRYPTION_KEY", "")
	t.Setenv("ENCRYPTION_KEY", "shared-platform-encryption-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EncryptionKey != "shared-platform-encryption-key" || !cfg.EncryptionEnabled() {
		t.Fatalf("EncryptionKey = %q", cfg.EncryptionKey)
	}
}

func TestMomentoPrefixWinsOverTheAlias(t *testing.T) {
	withRequiredEnvironment(t)
	t.Setenv("MOMENTO_ENCRYPTION_KEY", "momento-specific-encryption-key")
	t.Setenv("ENCRYPTION_KEY", "shared-platform-encryption-key")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EncryptionKey != "momento-specific-encryption-key" {
		t.Fatalf("EncryptionKey = %q", cfg.EncryptionKey)
	}
}

func TestPreviousEncryptionKeysAreSplitAndTrimmed(t *testing.T) {
	withRequiredEnvironment(t)
	t.Setenv("MOMENTO_ENCRYPTION_KEY", "current-encryption-key-value")
	t.Setenv("MOMENTO_ENCRYPTION_KEY_PREVIOUS", " first-old-key , second-old-key ,, ")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"first-old-key", "second-old-key"}
	if len(cfg.PreviousEncryptionKeys) != len(want) {
		t.Fatalf("PreviousEncryptionKeys = %v, want %v", cfg.PreviousEncryptionKeys, want)
	}
	for i, value := range want {
		if cfg.PreviousEncryptionKeys[i] != value {
			t.Fatalf("PreviousEncryptionKeys[%d] = %q, want %q", i, cfg.PreviousEncryptionKeys[i], value)
		}
	}
}

func TestEncryptionStaysOptional(t *testing.T) {
	withRequiredEnvironment(t)
	t.Setenv("MOMENTO_ENCRYPTION_KEY", "")
	t.Setenv("ENCRYPTION_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.EncryptionEnabled() {
		t.Fatal("encryption must stay disabled when no key is configured")
	}
}

func TestShortBootstrapPasswordIsRejected(t *testing.T) {
	withRequiredEnvironment(t)
	t.Setenv("MOMENTO_BOOTSTRAP_ADMIN_PASSWORD", "short")

	if _, err := Load(); err == nil {
		t.Fatal("a short bootstrap password was accepted")
	}
}
