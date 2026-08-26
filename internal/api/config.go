package api

import (
	"fmt"
	"os"
)

// Config holds process configuration loaded from environment variables.
type Config struct {
	DatabaseURL    string
	JWTSecret      string
	PublicHostname string
	Port           string
}

// LoadConfig reads configuration from environment variables. DATABASE_URL,
// JWT_SECRET and PUBLIC_HOSTNAME are required; PORT defaults to "8080".
func LoadConfig() (*Config, error) {
	cfg := &Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		PublicHostname: os.Getenv("PUBLIC_HOSTNAME"),
		Port:           os.Getenv("PORT"),
	}

	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.JWTSecret == "" {
		return nil, fmt.Errorf("JWT_SECRET is required")
	}
	if cfg.PublicHostname == "" {
		return nil, fmt.Errorf("PUBLIC_HOSTNAME is required")
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	return cfg, nil
}
