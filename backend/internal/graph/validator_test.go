package graph

import (
	"errors"
	"testing"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestCanonicalizeEdgesAcceptsStrictThreeTierGraph(t *testing.T) {
	nodes := testNodes()
	edges, err := CanonicalizeEdgeKinds(nodes, []domain.CanvasEdge{
		{ID: "sf", Source: "source:device-a", Target: "filter:device-a"},
		{ID: "ft", Source: "filter:device-a", Target: "target:proxy-a"},
	})
	if err != nil {
		t.Fatalf("expected valid graph, got %v", err)
	}
	if edges[0].Kind != domain.EdgeSourceToFilter || edges[1].Kind != domain.EdgeFilterToTarget {
		t.Fatalf("unexpected canonical kinds: %#v", edges)
	}
}

func TestValidateEdgesRejectsFilterFromAnotherDevice(t *testing.T) {
	nodes := testNodes()
	err := ValidateEdges(nodes, []domain.CanvasEdge{{
		ID:     "foreign",
		Source: "source:device-a",
		Target: "filter:device-b",
	}})
	if !errors.Is(err, ErrForeignFilter) {
		t.Fatalf("expected ErrForeignFilter, got %v", err)
	}
}

func TestValidateEdgesRejectsWrongDirection(t *testing.T) {
	nodes := testNodes()
	err := ValidateEdges(nodes, []domain.CanvasEdge{{
		ID:     "invalid",
		Source: "target:proxy-a",
		Target: "filter:device-a",
	}})
	if !errors.Is(err, ErrInvalidEdgeDirection) {
		t.Fatalf("expected ErrInvalidEdgeDirection, got %v", err)
	}
}

func testNodes() []domain.CanvasNode {
	return []domain.CanvasNode{
		{ID: "source:device-a", Kind: domain.NodeKindSource, Data: domain.SourceNodeData{DeviceID: "device-a"}},
		{ID: "filter:device-a", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "feature-a", DeviceID: "device-a"}},
		{ID: "filter:device-b", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "feature-b", DeviceID: "device-b"}},
		{ID: "target:proxy-a", Kind: domain.NodeKindTarget, Data: domain.TargetNodeData{ProxyName: "proxy-a"}},
	}
}
