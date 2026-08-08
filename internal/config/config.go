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
}

func Load() (Config, error) {
	c := Config{
		PostgresDSN:       strings.TrimSpace(os.Getenv("MOMENTO_POSTGRES_DSN")),
		BootstrapAdmin:    strings.TrimSpace(os.Getenv("MOMENTO_BOOTSTRAP_ADMIN")),
		BootstrapPassword: os.Getenv("MOMENTO_BOOTSTRAP_ADMIN_PASSWORD"),
	}
	if c.PostgresDSN == "" || c.BootstrapAdmin == "" || c.BootstrapPassword == "" {
		return Config{}, errors.New("MOMENTO_POSTGRES_DSN, MOMENTO_BOOTSTRAP_ADMIN and MOMENTO_BOOTSTRAP_ADMIN_PASSWORD are required")
	}
	if len(c.BootstrapPassword) < 12 {
		return Config{}, errors.New("MOMENTO_BOOTSTRAP_ADMIN_PASSWORD must be at least 12 characters")
	}
	return c, nil
}
