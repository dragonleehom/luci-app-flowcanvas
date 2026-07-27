package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
)

func TestLiveCatalogAPIReturnsHistoricalFlowAndMihomoTarget(t *testing.T) {
	ctx := context.Background()
	database, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()
	now := time.Now().UTC()
	feature := domain.ObservedFeature{
		ConnectionID: "api-live-connection", SourceIP: "192.168.1.50", DestinationIP: "203.0.113.10",
		DestinationPort: 443, ObservedHost: "v.qq.com", Network: domain.NetworkTCP, TransportHint: "tls",
		OpenedAt: now.Add(-time.Minute), ObservedAt: now,
	}
	if _, err := database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{{Kind: domain.FeatureObserved, Feature: feature}}); err != nil {
		t.Fatalf("apply observed feature: %v", err)
	}
	feature.ObservedAt = now.Add(time.Second)
	if _, err := database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{{Kind: domain.FeatureClosed, Feature: feature}}); err != nil {
		t.Fatalf("close feature: %v", err)
	}

	catalog := telemetry.NewLiveCatalog(database)
	catalog.ReplaceTargets([]domain.Target{{
		ID: domain.TargetNodeID("tailscale0"), ProxyName: "tailscale0", ProxyType: "DIRECT", UDP: true, Alive: true, State: domain.StateActive,
	}}, now)
	server := NewServer(database, catalog)

	canvasRequest := httptest.NewRequest(http.MethodGet, "/api/v1/canvas", nil)
	canvasResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(canvasResponse, canvasRequest)
	if canvasResponse.Code != http.StatusOK {
		t.Fatalf("canvas response status: %d %s", canvasResponse.Code, canvasResponse.Body.String())
	}
	var canvasPayload struct {
		Data domain.CanvasSnapshot `json:"data"`
	}
	if err := json.Unmarshal(canvasResponse.Body.Bytes(), &canvasPayload); err != nil {
		t.Fatalf("decode canvas payload: %v", err)
	}
	if len(canvasPayload.Data.Nodes) != 3 {
		t.Fatalf("expected 3 live canvas nodes, got %+v", canvasPayload.Data.Nodes)
	}
	var filterData domain.FilterNodeData
	for _, node := range canvasPayload.Data.Nodes {
		if node.Kind != domain.NodeKindFilter {
			continue
		}
		encoded, _ := json.Marshal(node.Data)
		if err := json.Unmarshal(encoded, &filterData); err != nil {
			t.Fatalf("decode filter data: %v", err)
		}
	}
	if filterData.ObservedHost != "v.qq.com" || filterData.State != domain.StateInactive || filterData.ActiveConnections != 0 {
		t.Fatalf("unexpected historical filter node: %+v", filterData)
	}

	featuresRequest := httptest.NewRequest(http.MethodGet, "/api/v1/features", nil)
	featuresResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(featuresResponse, featuresRequest)
	if featuresResponse.Code != http.StatusOK || !strings.Contains(featuresResponse.Body.String(), "v.qq.com") {
		t.Fatalf("unexpected feature response: %d %s", featuresResponse.Code, featuresResponse.Body.String())
	}
	targetsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/targets", nil)
	targetsResponse := httptest.NewRecorder()
	server.Router().ServeHTTP(targetsResponse, targetsRequest)
	if targetsResponse.Code != http.StatusOK || !strings.Contains(targetsResponse.Body.String(), "tailscale0") {
		t.Fatalf("unexpected target response: %d %s", targetsResponse.Code, targetsResponse.Body.String())
	}
}
