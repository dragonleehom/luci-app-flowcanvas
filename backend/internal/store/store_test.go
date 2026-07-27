package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestSaveDefaultGraphAdvancesRevisionAndPersistsEdges(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()

	nodes := testCatalogNodes()
	persisted, err := database.SaveDefaultGraph(ctx, 0, nodes, domain.GraphSaveRequest{
		NodePositions: []domain.NodePosition{
			{ID: "source:device-a", Position: domain.Position{X: 100, Y: 120}},
			{ID: "filter:feature-a", Position: domain.Position{X: 420, Y: 120}},
			{ID: "target:proxy-a", Position: domain.Position{X: 760, Y: 120}},
		},
		Edges: []domain.CanvasEdge{
			{ID: "sf", Source: "source:device-a", Target: "filter:feature-a", Kind: domain.EdgeSourceToFilter},
			{ID: "ft", Source: "filter:feature-a", Target: "target:proxy-a", Kind: domain.EdgeFilterToTarget},
		},
	})
	if err != nil {
		t.Fatalf("save default graph: %v", err)
	}
	if persisted.Metadata.Revision != 1 || persisted.Metadata.ETag != "canvas-1" {
		t.Fatalf("unexpected canvas metadata: %+v", persisted.Metadata)
	}
	if len(persisted.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(persisted.Edges))
	}
	if position := persisted.Positions["filter:feature-a"]; position.X != 420 || position.Y != 120 {
		t.Fatalf("unexpected saved position: %+v", position)
	}

	_, err = database.SaveDefaultGraph(ctx, 0, nodes, domain.GraphSaveRequest{})
	if !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("expected revision conflict, got %v", err)
	}
}

func testCatalogNodes() []domain.CanvasNode {
	now := time.Now().UTC()
	return []domain.CanvasNode{
		{
			ID: "source:device-a", Kind: domain.NodeKindSource,
			Data: domain.SourceNodeData{DeviceID: "device-a", Label: "Device A", IP: "192.168.1.2", State: domain.StateActive, LastSeenAt: now},
		},
		{
			ID: "filter:feature-a", Kind: domain.NodeKindFilter,
			Data: domain.FilterNodeData{DeviceApplicationID: "feature-a", DeviceID: "device-a", ObservedHost: "example.com", Network: domain.NetworkTCP, State: domain.StateActive, Match: domain.MatchSpec{Kind: domain.MatchDomain, Value: "example.com"}, FirstSeenAt: now, LastSeenAt: now},
		},
		{
			ID: "target:proxy-a", Kind: domain.NodeKindTarget,
			Data: domain.TargetNodeData{ProxyName: "proxy-a", ProxyType: "DIRECT", State: domain.StateActive},
		},
	}
}
