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

	FeatureQueueCapacity int
	FeatureBatchSize     int
	FeatureFlushInterval time.Duration

	ARPPath              string
	DHCPLeasePath        string
	TopologyInterval     time.Duration
	ProxyRefreshInterval time.Duration

	DemoMode bool
}

func LoadFromEnvironment() (Config, error) {
	config := Config{
		ListenAddress:        envOrDefault("FLOWCANVAS_LISTEN", "127.0.0.1:16789"),
		DatabasePath:         envOrDefault("FLOWCANVAS_DB", "/tmp/flowcanvas/flowcanvas.db"),
		MihomoController:     envOrDefault("FLOWCANVAS_MIHOMO_CONTROLLER", "http://127.0.0.1:9090"),
		MihomoSecret:         os.Getenv("FLOWCANVAS_MIHOMO_SECRET"),
		ConnectionInterval:   durationOrDefault(os.Getenv("FLOWCANVAS_CONNECTION_INTERVAL"), 250*time.Millisecond),
		FeatureQueueCapacity: intOrDefault(os.Getenv("FLOWCANVAS_FEATURE_QUEUE_CAPACITY"), 8192),
		FeatureBatchSize:     intOrDefault(os.Getenv("FLOWCANVAS_FEATURE_BATCH_SIZE"), 256),
		FeatureFlushInterval: durationOrDefault(os.Getenv("FLOWCANVAS_FEATURE_FLUSH_INTERVAL"), 200*time.Millisecond),
		ARPPath:              envOrDefault("FLOWCANVAS_ARP_PATH", "/proc/net/arp"),
		DHCPLeasePath:        envOrDefault("FLOWCANVAS_DHCP_LEASE_PATH", "/tmp/dhcp.leases"),
		TopologyInterval:     durationOrDefault(os.Getenv("FLOWCANVAS_TOPOLOGY_INTERVAL"), 30*time.Second),
		ProxyRefreshInterval: durationOrDefault(os.Getenv("FLOWCANVAS_PROXY_REFRESH_INTERVAL"), 10*time.Second),
		DemoMode:             boolOrDefault(os.Getenv("FLOWCANVAS_DEMO"), false),
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
	if config.FeatureQueueCapacity < 128 || config.FeatureQueueCapacity > 1_000_000 {
		return Config{}, fmt.Errorf("FLOWCANVAS_FEATURE_QUEUE_CAPACITY must be between 128 and 1000000")
	}
	if config.FeatureBatchSize < 1 || config.FeatureBatchSize > config.FeatureQueueCapacity {
		return Config{}, fmt.Errorf("FLOWCANVAS_FEATURE_BATCH_SIZE must be between 1 and FLOWCANVAS_FEATURE_QUEUE_CAPACITY")
	}
	if config.FeatureFlushInterval < 10*time.Millisecond || config.FeatureFlushInterval > 10*time.Second {
		return Config{}, fmt.Errorf("FLOWCANVAS_FEATURE_FLUSH_INTERVAL must be between 10ms and 10s")
	}
	if config.TopologyInterval < time.Second || config.TopologyInterval > time.Hour {
		return Config{}, fmt.Errorf("FLOWCANVAS_TOPOLOGY_INTERVAL must be between 1s and 1h")
	}
	if config.ProxyRefreshInterval < time.Second || config.ProxyRefreshInterval > time.Hour {
		return Config{}, fmt.Errorf("FLOWCANVAS_PROXY_REFRESH_INTERVAL must be between 1s and 1h")
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

func intOrDefault(raw string, fallback int) int {
	if strings.TrimSpace(raw) == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
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
