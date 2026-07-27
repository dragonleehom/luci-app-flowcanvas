package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
)

func TestDiscoveryRefreshInvokesConfiguredHandler(t *testing.T) {
	database, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()
	catalog := telemetry.NewMemoryCatalog(nil, domain.DiscoveryStatus{ConnectionsUpdatedAt: time.Now().UTC()})
	server := NewServer(database, catalog)
	called := false
	server.SetDiscoveryRefreshHandler(func(_ context.Context) error {
		called = true
		return nil
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/refresh", nil)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if !called {
		t.Fatal("expected discovery refresh handler to be called")
	}
}

func TestDiscoveryRefreshReportsUnavailableWhenNotConfigured(t *testing.T) {
	database, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()
	server := NewServer(database, telemetry.NewMemoryCatalog(nil, domain.DiscoveryStatus{}))
	request := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/refresh", nil)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusNotImplemented {
		t.Fatalf("expected 501, got %d: %s", response.Code, response.Body.String())
	}
}

func TestCompilationValidateInvokesService(t *testing.T) {
	database, catalog, snapshot := compilationTestFixture(t)
	defer database.Close()
	service := &compilationServiceStub{validateResult: domain.CompilationResult{
		Compilation: domain.CompilationRecord{ID: "compile-preview", Status: domain.CompilationValidated},
		Preview:     domain.CompilationPreview{CanvasID: snapshot.Canvas.ID, CanvasRevision: snapshot.Canvas.Revision, ManagedYAML: "rules: []\n"},
	}}
	server := NewServer(database, catalog)
	server.SetCompilationService(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/compilations/validate", nil)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.validated.Canvas.ID != snapshot.Canvas.ID {
		t.Fatalf("validate received unexpected snapshot: %+v", service.validated)
	}
}

func TestCompilationApplyRequiresCurrentRevisionAndInvokesService(t *testing.T) {
	database, catalog, snapshot := compilationTestFixture(t)
	defer database.Close()
	service := &compilationServiceStub{applyResult: domain.CompilationResult{
		Compilation: domain.CompilationRecord{ID: "compile-apply", Status: domain.CompilationApplied},
		Preview:     domain.CompilationPreview{CanvasID: snapshot.Canvas.ID, CanvasRevision: snapshot.Canvas.Revision},
	}}
	server := NewServer(database, catalog)
	server.SetCompilationService(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/compilations/apply", nil)
	request.Header.Set("If-Match", `"canvas-1"`)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	if service.applied.Canvas.Revision != 1 {
		t.Fatalf("apply received unexpected snapshot: %+v", service.applied)
	}
}

func TestCompilationApplyRejectsStaleRevision(t *testing.T) {
	database, catalog, _ := compilationTestFixture(t)
	defer database.Close()
	service := &compilationServiceStub{}
	server := NewServer(database, catalog)
	server.SetCompilationService(service)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/compilations/apply", nil)
	request.Header.Set("If-Match", `"canvas-0"`)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", response.Code, response.Body.String())
	}
	if service.applied.Canvas.ID != "" {
		t.Fatal("stale request must not invoke apply service")
	}
}

func TestCompilationAuditLookup(t *testing.T) {
	database, catalog, _ := compilationTestFixture(t)
	defer database.Close()
	_, err := database.CreateCompilation(context.Background(), domain.CompilationRecord{
		ID: "compile-read", CanvasID: "default", CanvasRevision: 1, Status: domain.CompilationValidated, ManagedYAML: "rules: []\n", CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("create compilation audit: %v", err)
	}
	server := NewServer(database, catalog)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/compilations/compile-read", nil)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "compile-read") {
		t.Fatalf("expected audit record, got %d: %s", response.Code, response.Body.String())
	}
}

type compilationServiceStub struct {
	validateResult domain.CompilationResult
	applyResult    domain.CompilationResult
	validateErr    error
	applyErr       error
	validated      domain.CanvasSnapshot
	applied        domain.CanvasSnapshot
}

func (s *compilationServiceStub) Validate(_ context.Context, snapshot domain.CanvasSnapshot) (domain.CompilationResult, error) {
	s.validated = snapshot
	return s.validateResult, s.validateErr
}

func (s *compilationServiceStub) Apply(_ context.Context, snapshot domain.CanvasSnapshot) (domain.CompilationResult, error) {
	s.applied = snapshot
	return s.applyResult, s.applyErr
}

func compilationTestFixture(t *testing.T) (*store.Store, *telemetry.MemoryCatalog, domain.CanvasSnapshot) {
	t.Helper()
	ctx := context.Background()
	database, err := store.Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	now := time.Now().UTC()
	nodes := []domain.CanvasNode{
		{ID: "source:tv", Kind: domain.NodeKindSource, Data: domain.SourceNodeData{DeviceID: "tv", IP: "192.168.1.50", State: domain.StateActive, LastSeenAt: now}},
		{ID: "filter:qq", Kind: domain.NodeKindFilter, Data: domain.FilterNodeData{DeviceApplicationID: "qq", DeviceID: "tv", ObservedHost: "v.qq.com", Match: domain.MatchSpec{Kind: domain.MatchDomain, Value: "v.qq.com"}, State: domain.StateActive}},
		{ID: "target:us", Kind: domain.NodeKindTarget, Data: domain.TargetNodeData{ProxyName: "Proxy-US", Alive: true, State: domain.StateActive}},
	}
	catalog := telemetry.NewMemoryCatalog(nodes, domain.DiscoveryStatus{ConnectionsUpdatedAt: now})
	persisted, err := database.SaveDefaultGraph(ctx, 0, nodes, domain.GraphSaveRequest{Edges: []domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:qq", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:qq", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	}})
	if err != nil {
		database.Close()
		t.Fatalf("save fixture graph: %v", err)
	}
	return database, catalog, domain.CanvasSnapshot{Canvas: persisted.Metadata, Nodes: nodes, Edges: persisted.Edges}
}
