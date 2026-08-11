package config

import (
	"errors"
	"os"
	"strings"
)

type Config struct {
	PostgresDSN       string
	BootstrapAdmin    string
	BootstrapPassword string
	// EncryptionKey seals administrator supplied secrets such as API keys, so a
	// restart never loses them and nobody has to type them in again.
	EncryptionKey string
	// PreviousEncryptionKeys keeps retired keys readable while a rotation runs.
	PreviousEncryptionKeys []string
}

// EncryptionEnabled reports whether secrets can be stored recoverably.
func (c Config) EncryptionEnabled() bool { return c.EncryptionKey != "" }

func Load() (Config, error) {
	c := Config{
		PostgresDSN:            strings.TrimSpace(os.Getenv("MOMENTO_POSTGRES_DSN")),
		BootstrapAdmin:         strings.TrimSpace(os.Getenv("MOMENTO_BOOTSTRAP_ADMIN")),
		BootstrapPassword:      os.Getenv("MOMENTO_BOOTSTRAP_ADMIN_PASSWORD"),
		EncryptionKey:          firstValue("MOMENTO_ENCRYPTION_KEY", "ENCRYPTION_KEY"),
		PreviousEncryptionKeys: splitKeys(firstValue("MOMENTO_ENCRYPTION_KEY_PREVIOUS", "ENCRYPTION_KEY_PREVIOUS")),
	}
	if c.PostgresDSN == "" || c.BootstrapAdmin == "" || c.BootstrapPassword == "" {
		return Config{}, errors.New("MOMENTO_POSTGRES_DSN, MOMENTO_BOOTSTRAP_ADMIN and MOMENTO_BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	if len(c.BootstrapPassword) < 12 {
		return Config{}, errors.New("MOMENTO_BOOTSTRAP_ADMIN_PASSWORD must be at least 12 characters")
	}
	return c, nil
}

// firstValue reads the Momento prefixed variable first and accepts the bare name
// as an alias, because container platforms often inject a shared ENCRYPTION_KEY.
func firstValue(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func splitKeys(value string) []string {
	out := []string{}
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
