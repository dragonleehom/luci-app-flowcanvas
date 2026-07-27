package mihomo

import (
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestNormalizeConnectionPrefersSniffHostAndMarksQUIC(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	feature, ok := NormalizeConnection(Connection{
		ID: "connection-1",
		Metadata: Metadata{
			SourceIP:        "192.168.1.50",
			DestinationIP:   "203.0.113.10",
			DestinationPort: 443,
			Host:            "fallback.example.com",
			SniffHost:       "GoogleVideo.com.",
			Network:         "udp",
			SniffType:       "quic",
		},
		Start: now.Add(-time.Minute),
	}, now)
	if !ok {
		t.Fatal("expected normalized feature")
	}
	if feature.ObservedHost != "googlevideo.com" {
		t.Fatalf("unexpected host %q", feature.ObservedHost)
	}
	if feature.Network != domain.NetworkQUIC || feature.TransportHint != "quic" {
		t.Fatalf("unexpected network data: %+v", feature)
	}
}

func TestNormalizeConnectionRejectsUnclassifiedIPOnlyTraffic(t *testing.T) {
	_, ok := NormalizeConnection(Connection{
		ID: "connection-2",
		Metadata: Metadata{
			SourceIP:      "192.168.1.50",
			DestinationIP: "203.0.113.10",
			Network:       "tcp",
		},
	}, time.Now())
	if ok {
		t.Fatal("expected IP-only traffic to be omitted from Filter discovery")
	}
}

func TestTargetsFromCatalogUsesStableNodeID(t *testing.T) {
	targets := TargetsFromCatalog(ProxyCatalogResponse{Proxies: map[string]Proxy{
		"Tailscale": {Name: "tailscale0", Type: "DIRECT", Alive: true, UDP: true},
	}})
	if len(targets) != 1 {
		t.Fatalf("expected 1 target, got %d", len(targets))
	}
	if targets[0].ID != domain.TargetNodeID("tailscale0") || targets[0].State != domain.StateActive {
		t.Fatalf("unexpected target: %+v", targets[0])
	}
}
