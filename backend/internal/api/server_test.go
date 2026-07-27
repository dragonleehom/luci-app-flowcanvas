package api

import (
	"context"
	"net/http"
	"net/http/httptest"
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
