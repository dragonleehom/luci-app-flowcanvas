package telemetry

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
)

type DiscoveryStore interface {
	LoadDiscoveryResources(ctx context.Context) (store.DiscoveryResources, error)
}

type LiveCatalog struct {
	store DiscoveryStore

	mu               sync.RWMutex
	targets          map[string]domain.Target
	devicesUpdatedAt time.Time
	targetsUpdatedAt time.Time
}

func NewLiveCatalog(database DiscoveryStore) *LiveCatalog {
	return &LiveCatalog{
		store:   database,
		targets: make(map[string]domain.Target),
	}
}

func (c *LiveCatalog) Snapshot(ctx context.Context) ([]domain.CanvasNode, domain.DiscoveryStatus, error) {
	resources, err := c.store.LoadDiscoveryResources(ctx)
	if err != nil {
		return nil, domain.DiscoveryStatus{}, err
	}
	c.mu.RLock()
	targets := make([]domain.Target, 0, len(c.targets))
	for _, target := range c.targets {
		targets = append(targets, target)
	}
	devicesUpdatedAt := c.devicesUpdatedAt
	targetsUpdatedAt := c.targetsUpdatedAt
	c.mu.RUnlock()
	sort.Slice(targets, func(i, j int) bool { return targets[i].ProxyName < targets[j].ProxyName })

	nodes := make([]domain.CanvasNode, 0, len(resources.Devices)+len(resources.DeviceApplications)+len(targets))
	for index, device := range resources.Devices {
		nodes = append(nodes, domain.CanvasNode{
			ID:       domain.SourceNodeID(device.ID),
			Kind:     domain.NodeKindSource,
			Position: domain.Position{X: 80, Y: 80 + float64(index*170)},
			Data: domain.SourceNodeData{
				DeviceID:   device.ID,
				Label:      device.Name,
				IP:         device.IPAddress,
				MAC:        device.MAC,
				State:      device.State,
				LastSeenAt: device.LastSeen,
			},
		})
	}
	for index, feature := range resources.DeviceApplications {
		nodes = append(nodes, domain.CanvasNode{
			ID:       domain.FilterNodeID(feature.ID),
			Kind:     domain.NodeKindFilter,
			Position: domain.Position{X: 460, Y: 80 + float64(index*170)},
			Data: domain.FilterNodeData{
				DeviceApplicationID: feature.ID,
				DeviceID:            feature.DeviceID,
				ObservedHost:        feature.ObservedHost,
				Network:             feature.Network,
				TransportHint:       feature.TransportHint,
				State:               feature.State,
				ActiveConnections:   feature.ActiveConnections,
				Match:               feature.Match,
				FirstSeenAt:         feature.FirstSeen,
				LastSeenAt:          feature.LastSeen,
			},
		})
	}
	for index, target := range targets {
		nodes = append(nodes, domain.CanvasNode{
			ID:       target.ID,
			Kind:     domain.NodeKindTarget,
			Position: domain.Position{X: 840, Y: 80 + float64(index*170)},
			Data: domain.TargetNodeData{
				ProxyName: target.ProxyName,
				ProxyType: target.ProxyType,
				Alive:     target.Alive,
				UDP:       target.UDP,
				State:     target.State,
			},
		})
	}

	return nodes, domain.DiscoveryStatus{
		ConnectionsUpdatedAt: resources.ConnectionsUpdatedAt,
		DevicesUpdatedAt:     devicesUpdatedAt,
		TargetsUpdatedAt:     targetsUpdatedAt,
	}, nil
}

func (c *LiveCatalog) ReplaceTargets(targets []domain.Target, updatedAt time.Time) {
	byID := make(map[string]domain.Target, len(targets))
	for _, target := range targets {
		byID[target.ID] = target
	}
	c.mu.Lock()
	c.targets = byID
	c.targetsUpdatedAt = updatedAt.UTC()
	c.mu.Unlock()
}

func (c *LiveCatalog) MarkDevicesRefreshed(updatedAt time.Time) {
	c.mu.Lock()
	c.devicesUpdatedAt = updatedAt.UTC()
	c.mu.Unlock()
}
