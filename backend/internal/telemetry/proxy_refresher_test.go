package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
)

func TestProxyRefresherUpdatesTargetCatalog(t *testing.T) {
	catalog := &recordingTargetCatalog{}
	refresher, err := NewProxyRefresher(fakeProxyCatalogSource{response: mihomo.ProxyCatalogResponse{Proxies: map[string]mihomo.Proxy{
		"Direct": {Name: "tailscale0", Type: "DIRECT", UDP: true, Alive: true},
		"Group":  {Name: "Proxy-US", Type: "URLTest", UDP: true, Alive: false},
	}}}, catalog, nil, nil)
	if err != nil {
		t.Fatalf("create proxy refresher: %v", err)
	}
	targets, err := refresher.Refresh(context.Background())
	if err != nil {
		t.Fatalf("refresh proxy catalog: %v", err)
	}
	if len(targets) != 2 || len(catalog.targets) != 2 {
		t.Fatalf("unexpected targets: %+v", targets)
	}
	var active, inactive int
	for _, target := range targets {
		switch target.State {
		case domain.StateActive:
			active++
		case domain.StateInactive:
			inactive++
		}
	}
	if active != 1 || inactive != 1 {
		t.Fatalf("unexpected target states: %+v", targets)
	}
}

type fakeProxyCatalogSource struct {
	response mihomo.ProxyCatalogResponse
}

func (s fakeProxyCatalogSource) Proxies(_ context.Context) (mihomo.ProxyCatalogResponse, error) {
	return s.response, nil
}

type recordingTargetCatalog struct {
	targets []domain.Target
}

func (c *recordingTargetCatalog) ReplaceTargets(targets []domain.Target, _ time.Time) {
	c.targets = append([]domain.Target(nil), targets...)
}
