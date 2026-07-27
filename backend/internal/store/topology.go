package store

import (
	"context"
	"fmt"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func (s *Store) UpsertTopologyDevices(ctx context.Context, devices []domain.Device, observedAt time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin topology transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	changed := false
	observedAt = observedAt.UTC()
	for _, device := range devices {
		if device.IPAddress == "" {
			continue
		}
		deviceID := domain.StableID("dev", device.IPAddress)
		name := device.Name
		if name == "" {
			name = "设备 " + device.IPAddress
		}
		result, err := tx.ExecContext(ctx, `
			INSERT INTO devices(
				id, ip_address, mac_address, display_name, hostname, state,
				first_seen_at, last_seen_at, updated_at
			) VALUES(?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), 'active', ?, ?, ?)
			ON CONFLICT(ip_address) DO UPDATE SET
				mac_address = COALESCE(NULLIF(excluded.mac_address, ''), devices.mac_address),
				display_name = CASE
					WHEN excluded.hostname IS NOT NULL AND excluded.hostname <> '' THEN excluded.display_name
					WHEN devices.display_name IS NULL OR devices.display_name = '' THEN excluded.display_name
					ELSE devices.display_name END,
				hostname = COALESCE(NULLIF(excluded.hostname, ''), devices.hostname),
				state = 'active',
				last_seen_at = excluded.last_seen_at,
				updated_at = excluded.updated_at`,
			deviceID, device.IPAddress, device.MAC, name, device.Hostname,
			observedAt.Unix(), observedAt.Unix(), observedAt.Unix(),
		)
		if err != nil {
			return false, fmt.Errorf("upsert topology device %q: %w", device.IPAddress, err)
		}
		if rows, _ := result.RowsAffected(); rows > 0 {
			changed = true
		}
	}

	// A source with an active dynamic application stays active even if ARP has not
	// refreshed yet. Every other absent device is retained as a historical node.
	result, err := tx.ExecContext(ctx, `
		UPDATE devices
		SET state = 'inactive', updated_at = ?
		WHERE state <> 'inactive'
		  AND last_seen_at < ?
		  AND NOT EXISTS (
				SELECT 1 FROM device_applications
				WHERE device_applications.device_id = devices.id
				  AND device_applications.active_connections > 0
			  )`, observedAt.Unix(), observedAt.Unix())
	if err != nil {
		return false, fmt.Errorf("mark stale topology devices inactive: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows > 0 {
		changed = true
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit topology transaction: %w", err)
	}
	return changed, nil
}
