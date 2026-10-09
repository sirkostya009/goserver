package config

import "os"

type Config struct {
	Addr        string
	DatabaseURL string
	// LogFormat is "dev" for short lines, "json" for production.
	LogFormat string
	// AdminToken is the operator's bearer token. Empty closes the operator's
	// routes to everyone.
	AdminToken string
}

// MinAdminTokenLen keeps the operator's token out of guessing range.
const MinAdminTokenLen = 32

func Parse() Config {
	return Config{
		Addr:        env("ADDR", ":8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://postgres@localhost:5432/goserver?sslmode=disable"),
		LogFormat:   env("LOG_FORMAT", "dev"),
		AdminToken:  os.Getenv("ADMIN_TOKEN"),
	}
}

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
