package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
)

func TestLiveCatalogBuildsSourceFilterAndTargetNodes(t *testing.T) {
	now := time.Date(2026, 7, 27, 13, 0, 0, 0, time.UTC)
	device := domain.Device{ID: "dev-a", IPAddress: "192.168.1.50", Name: "TV", MAC: "00:11:22:33:44:55", State: domain.StateInactive, LastSeen: now}
	feature := domain.DeviceApplication{
		ID: "feature-a", DeviceID: "dev-a", ObservedHost: "v.qq.com", Network: domain.NetworkTCP,
		State: domain.StateInactive, ActiveConnections: 0,
		Match: domain.MatchSpec{Kind: domain.MatchDomain, Value: "v.qq.com"}, FirstSeen: now.Add(-time.Hour), LastSeen: now,
	}
	catalog := NewLiveCatalog(fakeDiscoveryStore{resources: store.DiscoveryResources{
		Devices:              []domain.Device{device},
		DeviceApplications:   []domain.DeviceApplication{feature},
		ConnectionsUpdatedAt: now,
	}})
	catalog.ReplaceTargets([]domain.Target{{
		ID: domain.TargetNodeID("tailscale0"), ProxyName: "tailscale0", ProxyType: "DIRECT", UDP: true, Alive: true, State: domain.StateActive,
	}}, now)
	catalog.MarkDevicesRefreshed(now)

	nodes, discovery, err := catalog.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot live catalog: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected source, filter and target nodes, got %+v", nodes)
	}
	if nodes[0].Kind != domain.NodeKindSource || nodes[1].Kind != domain.NodeKindFilter || nodes[2].Kind != domain.NodeKindTarget {
		t.Fatalf("unexpected node order/types: %+v", nodes)
	}
	if discovery.ConnectionsUpdatedAt != now || discovery.DevicesUpdatedAt != now || discovery.TargetsUpdatedAt != now {
		t.Fatalf("unexpected discovery timestamps: %+v", discovery)
	}
}

type fakeDiscoveryStore struct {
	resources store.DiscoveryResources
}

func (s fakeDiscoveryStore) LoadDiscoveryResources(_ context.Context) (store.DiscoveryResources, error) {
	return s.resources, nil
}
