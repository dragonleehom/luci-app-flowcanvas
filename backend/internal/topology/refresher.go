package topology

import (
	"context"
	"fmt"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

type DeviceStore interface {
	UpsertTopologyDevices(ctx context.Context, devices []domain.Device, observedAt time.Time) (bool, error)
}

type Refresher struct {
	store       DeviceStore
	arpPath     string
	leasePath   string
	onRefreshed func(changed bool, observedAt time.Time)
	onError     func(error)
}

func NewRefresher(store DeviceStore, arpPath, leasePath string, onRefreshed func(bool, time.Time), onError func(error)) (*Refresher, error) {
	if store == nil {
		return nil, fmt.Errorf("topology refresher requires a device store")
	}
	if arpPath == "" {
		return nil, fmt.Errorf("topology refresher requires an ARP path")
	}
	return &Refresher{
		store:       store,
		arpPath:     arpPath,
		leasePath:   leasePath,
		onRefreshed: onRefreshed,
		onError:     onError,
	}, nil
}

func (r *Refresher) Refresh(ctx context.Context) (bool, error) {
	now := time.Now().UTC()
	neighbors, err := ReadARP(r.arpPath)
	if err != nil {
		return false, err
	}
	leases, err := ReadDnsmasqLeases(r.leasePath, now)
	if err != nil {
		return false, err
	}
	devices := BuildDevices(neighbors, leases, now)
	changed, err := r.store.UpsertTopologyDevices(ctx, devices, now)
	if err != nil {
		return false, err
	}
	if r.onRefreshed != nil {
		r.onRefreshed(changed, now)
	}
	return changed, nil
}

func (r *Refresher) Run(ctx context.Context, interval time.Duration) {
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

func (r *Refresher) refresh(ctx context.Context) {
	if _, err := r.Refresh(ctx); err != nil && r.onError != nil && ctx.Err() == nil {
		r.onError(err)
	}
}
