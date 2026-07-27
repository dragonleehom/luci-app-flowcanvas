package telemetry

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

type Catalog interface {
	Snapshot(ctx context.Context) ([]domain.CanvasNode, domain.DiscoveryStatus, error)
}

type MemoryCatalog struct {
	mu        sync.RWMutex
	nodes     map[string]domain.CanvasNode
	discovery domain.DiscoveryStatus
}

func NewMemoryCatalog(nodes []domain.CanvasNode, discovery domain.DiscoveryStatus) *MemoryCatalog {
	catalog := &MemoryCatalog{nodes: make(map[string]domain.CanvasNode, len(nodes)), discovery: discovery}
	catalog.Replace(nodes, discovery)
	return catalog
}

func (c *MemoryCatalog) Snapshot(_ context.Context) ([]domain.CanvasNode, domain.DiscoveryStatus, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	nodes := make([]domain.CanvasNode, 0, len(c.nodes))
	for _, node := range c.nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, c.discovery, nil
}

func (c *MemoryCatalog) Replace(nodes []domain.CanvasNode, discovery domain.DiscoveryStatus) {
	copyByID := make(map[string]domain.CanvasNode, len(nodes))
	for _, node := range nodes {
		copyByID[node.ID] = node
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes = copyByID
	c.discovery = discovery
}

func NewDemoCatalog(now time.Time) *MemoryCatalog {
	deviceID := domain.StableID("dev", "192.168.1.50|00:11:22:33:44:55")
	applicationID := domain.StableID("app", "v.qq.com")
	deviceApplicationID := domain.StableID("da", deviceID+"|"+applicationID+"|tcp")
	targetName := "tailscale0"

	nodes := []domain.CanvasNode{
		{
			ID:       domain.SourceNodeID(deviceID),
			Kind:     domain.NodeKindSource,
			Position: domain.Position{X: 80, Y: 180},
			Data: domain.SourceNodeData{
				DeviceID: deviceID,
				Label:    "LivingRoom-TV",
				IP:       "192.168.1.50",
				MAC:      "00:11:22:33:44:55",
				State:    domain.StateActive,
				LastSeenAt: now.UTC(),
			},
		},
		{
			ID:       domain.FilterNodeID(deviceApplicationID),
			Kind:     domain.NodeKindFilter,
			Position: domain.Position{X: 460, Y: 180},
			Data: domain.FilterNodeData{
				DeviceApplicationID: deviceApplicationID,
				DeviceID:            deviceID,
				ObservedHost:        "v.qq.com",
				Network:             domain.NetworkTCP,
				TransportHint:       "tls",
				State:               domain.StateActive,
				ActiveConnections:   2,
				Match:               domain.MatchSpec{Kind: domain.MatchDomain, Value: "v.qq.com"},
				FirstSeenAt:         now.Add(-2 * time.Hour).UTC(),
				LastSeenAt:          now.UTC(),
			},
		},
		{
			ID:       domain.TargetNodeID(targetName),
			Kind:     domain.NodeKindTarget,
			Position: domain.Position{X: 840, Y: 180},
			Data: domain.TargetNodeData{
				ProxyName: targetName,
				ProxyType: "DIRECT",
				Alive:     true,
				UDP:       true,
				State:     domain.StateActive,
			},
		},
	}
	return NewMemoryCatalog(nodes, domain.DiscoveryStatus{
		ConnectionsUpdatedAt: now.UTC(),
		DevicesUpdatedAt:     now.UTC(),
		TargetsUpdatedAt:     now.UTC(),
	})
}
