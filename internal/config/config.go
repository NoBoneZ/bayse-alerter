package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	BayseBaseURL   string
	BaysePublicKey string
	DatabaseURL    string
	PollInterval   time.Duration
	HTTPAddr       string
	HTTPTimeout    time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		BayseBaseURL: getEnv("BAYSE_BASE_URL", "https://relay.bayse.markets/v1/pm"),
		HTTPAddr:     getEnv("HTTP_ADDR", ":8080"),
	}

	var err error
	if cfg.BaysePublicKey, err = requireEnv("BAYSE_PUBLIC_KEY"); err != nil {
		return Config{}, err
	}
	if cfg.DatabaseURL, err = requireEnv("DATABASE_URL"); err != nil {
		return Config{}, err
	}

	if cfg.PollInterval, err = getDuration("POLL_INTERVAL", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.HTTPTimeout, err = getDuration("HTTP_TIMEOUT", 5*time.Second); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func requireEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("config: required env var %s is not set", key)
	}
	return v, nil
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("config: %s must be a duration like 10s: %w", key, err)
	}
	return d, nil
}
