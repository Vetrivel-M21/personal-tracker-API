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
	// GoogleClientID is optional - unset simply means "Sign in with Google"
	// is unavailable (handleGoogleLogin returns a clear 500 if it's hit
	// without one configured), so existing deployments aren't required to
	// set it.
	GoogleClientID string
	// SMTP* are optional at boot, same as GoogleClientID - unset means
	// handleSignup 500s with a clear message instead of trying (and failing)
	// to send a verification email.
	SMTPHost     string
	SMTPPort     string
	SMTPUsername string
	SMTPPassword string
	SMTPFrom     string
}

// LoadConfig reads configuration from environment variables. DATABASE_URL,
// JWT_SECRET and PUBLIC_HOSTNAME are required; PORT defaults to "8080".
func LoadConfig() (*Config, error) {
	cfg := &Config{
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		PublicHostname: os.Getenv("PUBLIC_HOSTNAME"),
		Port:           os.Getenv("PORT"),
		GoogleClientID: os.Getenv("GOOGLE_CLIENT_ID"),
		SMTPHost:       os.Getenv("SMTP_HOST"),
		SMTPPort:       os.Getenv("SMTP_PORT"),
		SMTPUsername:   os.Getenv("SMTP_USERNAME"),
		SMTPPassword:   os.Getenv("SMTP_PASSWORD"),
		SMTPFrom:       os.Getenv("SMTP_FROM"),
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
