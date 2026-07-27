package compiler

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestCompileGroupsPayloadByTargetAndRendersDeterministicOverlay(t *testing.T) {
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f-domain", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t-domain", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
		{ID: "s-f-suffix", Source: "source:tv", Target: "filter:suffix", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t-suffix", Source: "filter:suffix", Target: "target:us", Kind: domain.EdgeFilterToTarget},
		{ID: "s-f-keyword", Source: "source:tv", Target: "filter:keyword", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t-keyword", Source: "filter:keyword", Target: "target:direct", Kind: domain.EdgeFilterToTarget},
	})

	preview, err := Compile(snapshot)
	if err != nil {
		t.Fatalf("compile valid snapshot: %v", err)
	}
	if len(preview.Providers) != 2 || len(preview.Rules) != 2 {
		t.Fatalf("expected two grouped providers/rules, got %+v", preview)
	}
	usProvider := findProvider(t, preview.Providers, "Proxy-US")
	if want := []string{
		"AND,((SRC-IP-CIDR,192.168.1.50/32),(DOMAIN,v.qq.com))",
		"AND,((SRC-IP-CIDR,192.168.1.50/32),(DOMAIN-SUFFIX,qq.com))",
	}; strings.Join(usProvider.Payload, "\n") != strings.Join(want, "\n") {
		t.Fatalf("unexpected Proxy-US payload: %#v", usProvider.Payload)
	}
	directProvider := findProvider(t, preview.Providers, "DIRECT")
	if len(directProvider.Payload) != 1 || directProvider.Payload[0] != "AND,((SRC-IP-CIDR,192.168.1.50/32),(DOMAIN-KEYWORD,googlevideo))" {
		t.Fatalf("unexpected DIRECT payload: %#v", directProvider.Payload)
	}
	if !strings.Contains(preview.ManagedYAML, "rule-providers:") || !strings.Contains(preview.ManagedYAML, "RULE-SET,") {
		t.Fatalf("managed YAML is incomplete: %s", preview.ManagedYAML)
	}
	var parsed yaml.Node
	if err := yaml.Unmarshal([]byte(preview.ManagedYAML), &parsed); err != nil {
		t.Fatalf("managed YAML should parse: %v", err)
	}
	if preview.ContentHash == "" {
		t.Fatal("expected a content hash")
	}
}

func TestCompileWarnsForInactiveTarget(t *testing.T) {
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:domain", Target: "target:offline", Kind: domain.EdgeFilterToTarget},
	})
	snapshot.Nodes = append(snapshot.Nodes, domain.CanvasNode{
		ID: "target:offline", Kind: domain.NodeKindTarget,
		Data: domain.TargetNodeData{ProxyName: "Proxy-Offline", ProxyType: "Selector", Alive: false, State: domain.StateInactive},
	})
	preview, err := Compile(snapshot)
	if err != nil {
		t.Fatalf("inactive target should only warn: %v", err)
	}
	if len(preview.Warnings) != 1 || !strings.Contains(preview.Warnings[0], "Proxy-Offline") {
		t.Fatalf("expected inactive target warning, got %+v", preview.Warnings)
	}
}

func TestCompileRejectsAmbiguousFilterTarget(t *testing.T) {
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-us", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
		{ID: "f-direct", Source: "filter:domain", Target: "target:direct", Kind: domain.EdgeFilterToTarget},
	})
	_, err := Compile(snapshot)
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if validation.Code != "FILTER_TARGET_AMBIGUOUS" {
		t.Fatalf("unexpected validation code: %+v", validation)
	}
}

func TestCompileRejectsUnsafeInput(t *testing.T) {
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	})
	for index := range snapshot.Nodes {
		if snapshot.Nodes[index].ID == "source:tv" {
			snapshot.Nodes[index].Data = domain.SourceNodeData{DeviceID: "tv", IP: "not-an-ip"}
		}
	}
	_, err := Compile(snapshot)
	var validation *ValidationError
	if !errors.As(err, &validation) || validation.Code != "INVALID_SOURCE_IP" {
		t.Fatalf("expected invalid source IP, got %v", err)
	}
}

func findProvider(t *testing.T, providers []domain.CompiledProvider, target string) domain.CompiledProvider {
	t.Helper()
	for _, provider := range providers {
		if provider.TargetName == target {
			return provider
		}
	}
	t.Fatalf("provider for %s not found in %+v", target, providers)
	return domain.CompiledProvider{}
}

func compilationSnapshot(edges []domain.CanvasEdge) domain.CanvasSnapshot {
	now := time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC)
	return domain.CanvasSnapshot{
		Canvas: domain.CanvasMetadata{ID: "default", Revision: 7, UpdatedAt: now},
		Nodes: []domain.CanvasNode{
			{ID: "source:tv", Kind: domain.NodeKindSource, Data: domain.SourceNodeData{DeviceID: "tv", Label: "TV", IP: "192.168.1.50", State: domain.StateActive, LastSeenAt: now}},
			{ID: "filter:domain", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "domain", DeviceID: "tv", ObservedHost: "v.qq.com", Match: domain.MatchSpec{Kind: domain.MatchDomain, Value: "v.qq.com"}, State: domain.StateActive}},
			{ID: "filter:suffix", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "suffix", DeviceID: "tv", ObservedHost: "video.qq.com", Match: domain.MatchSpec{Kind: domain.MatchSuffix, Value: "qq.com"}, State: domain.StateInactive}},
			{ID: "filter:keyword", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "keyword", DeviceID: "tv", ObservedHost: "googlevideo.com", Match: domain.MatchSpec{Kind: domain.MatchKeyword, Value: "googlevideo"}, State: domain.StateActive}},
			{ID: "target:us", Kind: domain.NodeKindTarget, Data: domain.TargetNodeData{ProxyName: "Proxy-US", ProxyType: "Selector", Alive: true, State: domain.StateActive}},
			{ID: "target:direct", Kind: domain.NodeKindTarget, Data: domain.TargetNodeData{ProxyName: "DIRECT", ProxyType: "DIRECT", Alive: true, State: domain.StateActive}},
		},
		Edges: edges,
	}
}
