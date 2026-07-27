package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	flowapi "github.com/dragonleehom/luci-app-flowcanvas/backend/internal/api"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/compiler"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
)

func TestCompilationControlPlaneAppliesManagedRulesToMihomo(t *testing.T) {
	var reloads atomic.Int32
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer integration-secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/configs":
			if r.Method != http.MethodPut || r.URL.Query().Get("force") != "true" {
				http.Error(w, "bad reload request", http.StatusBadRequest)
				return
			}
			reloads.Add(1)
			w.WriteHeader(http.StatusNoContent)
		case "/proxies":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"proxies":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer controller.Close()

	ctx := context.Background()
	database, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()
	now := time.Now().UTC()
	nodes := []domain.CanvasNode{
		{ID: "source:tv", Kind: domain.NodeKindSource, Data: domain.SourceNodeData{DeviceID: "tv", Label: "TV", IP: "192.168.1.50", State: domain.StateActive, LastSeenAt: now}},
		{ID: "filter:qq", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "qq", DeviceID: "tv", ObservedHost: "v.qq.com", Match: domain.MatchSpec{Kind: domain.MatchDomain, Value: "v.qq.com"}, State: domain.StateActive}},
		{ID: "target:us", Kind: domain.NodeKindTarget, Data: domain.TargetNodeData{ProxyName: "Proxy-US", ProxyType: "Selector", Alive: true, State: domain.StateActive}},
	}
	catalog := telemetry.NewMemoryCatalog(nodes, domain.DiscoveryStatus{ConnectionsUpdatedAt: now})
	_, err = database.SaveDefaultGraph(ctx, 0, nodes, domain.GraphSaveRequest{Edges: []domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:qq", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:qq", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	}})
	if err != nil {
		t.Fatalf("save graph: %v", err)
	}

	directory := t.TempDir()
	configPath := filepath.Join(directory, "mihomo.yaml")
	original := []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n")
	if err := os.WriteFile(configPath, original, 0o640); err != nil {
		t.Fatalf("write Mihomo config: %v", err)
	}
	client, err := mihomo.NewClient(controller.URL, "integration-secret", controller.Client())
	if err != nil {
		t.Fatalf("create controller client: %v", err)
	}
	service, err := compiler.NewService(database, client, compiler.ApplyOptions{
		ConfigPath:      configPath,
		BackupDirectory: filepath.Join(directory, "backups"),
		Timeout:         2 * time.Second,
	})
	if err != nil {
		t.Fatalf("create compilation service: %v", err)
	}
	server := flowapi.NewServer(database, catalog)
	server.SetCompilationService(service)
	controlPlane := httptest.NewServer(server.Router())
	defer controlPlane.Close()

	validateResponse, err := http.Post(controlPlane.URL+"/api/v1/compilations/validate", "application/json", nil)
	if err != nil {
		t.Fatalf("validate request: %v", err)
	}
	if validateResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected validate 200, got %s", validateResponse.Status)
	}
	_ = validateResponse.Body.Close()

	request, err := http.NewRequest(http.MethodPost, controlPlane.URL+"/api/v1/compilations/apply", nil)
	if err != nil {
		t.Fatalf("create apply request: %v", err)
	}
	request.Header.Set("If-Match", `"canvas-1"`)
	response, err := controlPlane.Client().Do(request)
	if err != nil {
		t.Fatalf("apply request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected apply 200, got %s", response.Status)
	}
	var body struct {
		Data domain.CompilationResult `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode apply response: %v", err)
	}
	if body.Data.Compilation.Status != domain.CompilationApplied || body.Data.Rollback == nil {
		t.Fatalf("unexpected apply result: %+v", body.Data)
	}
	if reloads.Load() != 1 {
		t.Fatalf("expected one Mihomo reload, got %d", reloads.Load())
	}
	candidate, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read candidate config: %v", err)
	}
	if !strings.Contains(string(candidate), "flowcanvas-") || !strings.Contains(string(candidate), "DOMAIN,v.qq.com") {
		t.Fatalf("candidate config does not contain generated rule:\n%s", candidate)
	}

	auditResponse, err := http.Get(controlPlane.URL + "/api/v1/compilations/" + body.Data.Compilation.ID)
	if err != nil {
		t.Fatalf("read audit request: %v", err)
	}
	defer auditResponse.Body.Close()
	if auditResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected audit 200, got %s", auditResponse.Status)
	}
}
