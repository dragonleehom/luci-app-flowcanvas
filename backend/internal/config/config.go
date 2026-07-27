package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddress      string
	DatabasePath       string
	MihomoController   string
	MihomoSecret       string
	ConnectionInterval time.Duration
	DemoMode           bool
}

func LoadFromEnvironment() (Config, error) {
	config := Config{
		ListenAddress:      envOrDefault("FLOWCANVAS_LISTEN", "127.0.0.1:16789"),
		DatabasePath:       envOrDefault("FLOWCANVAS_DB", "/tmp/flowcanvas/flowcanvas.db"),
		MihomoController:   envOrDefault("FLOWCANVAS_MIHOMO_CONTROLLER", "http://127.0.0.1:9090"),
		MihomoSecret:       os.Getenv("FLOWCANVAS_MIHOMO_SECRET"),
		ConnectionInterval: durationOrDefault(os.Getenv("FLOWCANVAS_CONNECTION_INTERVAL"), 250*time.Millisecond),
		DemoMode:           boolOrDefault(os.Getenv("FLOWCANVAS_DEMO"), true),
	}
	if config.ListenAddress == "" {
		return Config{}, fmt.Errorf("FLOWCANVAS_LISTEN must not be empty")
	}
	if config.DatabasePath == "" {
		return Config{}, fmt.Errorf("FLOWCANVAS_DB must not be empty")
	}
	if config.MihomoController == "" {
		return Config{}, fmt.Errorf("FLOWCANVAS_MIHOMO_CONTROLLER must not be empty")
	}
	if config.ConnectionInterval < 100*time.Millisecond || config.ConnectionInterval > 10*time.Second {
		return Config{}, fmt.Errorf("FLOWCANVAS_CONNECTION_INTERVAL must be between 100ms and 10s")
	}
	return config, nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationOrDefault(raw string, fallback time.Duration) time.Duration {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return fallback
	}
	return value
}

func boolOrDefault(raw string, fallback bool) bool {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}
