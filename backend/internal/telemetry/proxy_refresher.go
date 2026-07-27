package telemetry

import (
	"context"
	"fmt"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
)

type ProxyCatalogSource interface {
	Proxies(ctx context.Context) (mihomo.ProxyCatalogResponse, error)
}

type TargetCatalog interface {
	ReplaceTargets(targets []domain.Target, updatedAt time.Time)
}

type ProxyRefresher struct {
	source      ProxyCatalogSource
	catalog     TargetCatalog
	onRefreshed func(targets []domain.Target, updatedAt time.Time)
	onError     func(error)
}

func NewProxyRefresher(source ProxyCatalogSource, catalog TargetCatalog, onRefreshed func([]domain.Target, time.Time), onError func(error)) (*ProxyRefresher, error) {
	if source == nil {
		return nil, fmt.Errorf("proxy refresher requires a Mihomo proxy catalog source")
	}
	if catalog == nil {
		return nil, fmt.Errorf("proxy refresher requires a target catalog")
	}
	return &ProxyRefresher{source: source, catalog: catalog, onRefreshed: onRefreshed, onError: onError}, nil
}

func (r *ProxyRefresher) Refresh(ctx context.Context) ([]domain.Target, error) {
	response, err := r.source.Proxies(ctx)
	if err != nil {
		return nil, err
	}
	targets := mihomo.TargetsFromCatalog(response)
	now := time.Now().UTC()
	r.catalog.ReplaceTargets(targets, now)
	if r.onRefreshed != nil {
		r.onRefreshed(targets, now)
	}
	return targets, nil
}

func (r *ProxyRefresher) Run(ctx context.Context, interval time.Duration) {
	r.refresh(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refresh(ctx)
		}
	}
}

func (r *ProxyRefresher) refresh(ctx context.Context) {
	if _, err := r.Refresh(ctx); err != nil && r.onError != nil && ctx.Err() == nil {
		r.onError(err)
	}
}
