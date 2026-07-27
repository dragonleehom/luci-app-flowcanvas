package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

type FeatureApplyResult struct {
	Observed int
	Closed   int
	Changed  bool
}

type DiscoveryResources struct {
	Devices              []domain.Device
	DeviceApplications   []domain.DeviceApplication
	ConnectionsUpdatedAt time.Time
}

// ApplyFeatureEvents persists a coalesced set of Mihomo connection observations.
// A connection ID is authoritative for liveness: observed rows keep closed_at NULL,
// while closed rows stamp closed_at. The affected device/application combinations are
// then recomputed from the connection_samples truth table in the same transaction.
func (s *Store) ApplyFeatureEvents(ctx context.Context, events []domain.FeatureEvent) (FeatureApplyResult, error) {
	if len(events) == 0 {
		return FeatureApplyResult{}, nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return FeatureApplyResult{}, fmt.Errorf("begin feature event transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result := FeatureApplyResult{}
	affectedDeviceApplications := make(map[string]struct{})
	affectedApplications := make(map[string]struct{})
	now := time.Now().UTC()

	for _, event := range events {
		switch event.Kind {
		case domain.FeatureObserved:
			deviceApplicationID, applicationID, changed, err := applyObservedFeatureTx(ctx, tx, event.Feature, now)
			if err != nil {
				return FeatureApplyResult{}, err
			}
			affectedDeviceApplications[deviceApplicationID] = struct{}{}
			affectedApplications[applicationID] = struct{}{}
			result.Observed++
			result.Changed = result.Changed || changed
		case domain.FeatureClosed:
			deviceApplicationID, applicationID, found, changed, err := closeObservedFeatureTx(ctx, tx, event.Feature, now)
			if err != nil {
				return FeatureApplyResult{}, err
			}
			if found {
				affectedDeviceApplications[deviceApplicationID] = struct{}{}
				affectedApplications[applicationID] = struct{}{}
				result.Closed++
				result.Changed = result.Changed || changed
			}
		default:
			return FeatureApplyResult{}, fmt.Errorf("unsupported feature event kind %q", event.Kind)
		}
	}

	for deviceApplicationID := range affectedDeviceApplications {
		if err := reconcileDeviceApplicationTx(ctx, tx, deviceApplicationID, now); err != nil {
			return FeatureApplyResult{}, err
		}
	}
	for applicationID := range affectedApplications {
		if err := reconcileApplicationTx(ctx, tx, applicationID, now); err != nil {
			return FeatureApplyResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return FeatureApplyResult{}, fmt.Errorf("commit feature event transaction: %w", err)
	}
	return result, nil
}

func (s *Store) MarkOpenConnectionsInactive(ctx context.Context, closedAt time.Time) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin stale connection cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `
		UPDATE connection_samples
		SET closed_at = ?
		WHERE closed_at IS NULL`, closedAt.UTC().Unix())
	if err != nil {
		return 0, fmt.Errorf("close stale connections: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT id, application_id FROM device_applications
		WHERE active_connections > 0 OR state = 'active'`)
	if err != nil {
		return 0, fmt.Errorf("load active device applications for cleanup: %w", err)
	}
	defer rows.Close()
	deviceApplications := make([]string, 0)
	applications := make(map[string]struct{})
	for rows.Next() {
		var deviceApplicationID, applicationID string
		if err := rows.Scan(&deviceApplicationID, &applicationID); err != nil {
			return 0, fmt.Errorf("scan stale device application: %w", err)
		}
		deviceApplications = append(deviceApplications, deviceApplicationID)
		applications[applicationID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate stale device applications: %w", err)
	}
	for _, deviceApplicationID := range deviceApplications {
		if err := reconcileDeviceApplicationTx(ctx, tx, deviceApplicationID, closedAt.UTC()); err != nil {
			return 0, err
		}
	}
	for applicationID := range applications {
		if err := reconcileApplicationTx(ctx, tx, applicationID, closedAt.UTC()); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit stale connection cleanup: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read stale connection cleanup result: %w", err)
	}
	return count, nil
}

func (s *Store) LoadDiscoveryResources(ctx context.Context) (DiscoveryResources, error) {
	resources := DiscoveryResources{
		Devices:            make([]domain.Device, 0),
		DeviceApplications: make([]domain.DeviceApplication, 0),
	}
	deviceRows, err := s.db.QueryContext(ctx, `
		SELECT id, ip_address, COALESCE(mac_address, ''), display_name, COALESCE(hostname, ''),
		       state, first_seen_at, last_seen_at
		FROM devices
		ORDER BY CASE state WHEN 'active' THEN 0 WHEN 'unknown' THEN 1 ELSE 2 END,
		         last_seen_at DESC, ip_address`)
	if err != nil {
		return DiscoveryResources{}, fmt.Errorf("query discovered devices: %w", err)
	}
	defer deviceRows.Close()
	for deviceRows.Next() {
		var device domain.Device
		var state string
		var firstSeen, lastSeen int64
		if err := deviceRows.Scan(
			&device.ID, &device.IPAddress, &device.MAC, &device.Name, &device.Hostname,
			&state, &firstSeen, &lastSeen,
		); err != nil {
			return DiscoveryResources{}, fmt.Errorf("scan discovered device: %w", err)
		}
		device.State = domain.ResourceState(state)
		device.FirstSeen = time.Unix(firstSeen, 0).UTC()
		device.LastSeen = time.Unix(lastSeen, 0).UTC()
		resources.Devices = append(resources.Devices, device)
	}
	if err := deviceRows.Err(); err != nil {
		return DiscoveryResources{}, fmt.Errorf("iterate discovered devices: %w", err)
	}

	featureRows, err := s.db.QueryContext(ctx, `
		SELECT da.id, da.device_id, a.id, a.observed_host, da.network,
		       COALESCE(da.transport_hint, ''), COALESCE(da.destination_ip, ''),
		       COALESCE(da.destination_port, 0), da.state, da.active_connections,
		       a.match_kind, a.match_value, da.first_seen_at, da.last_seen_at
		FROM device_applications AS da
		JOIN applications AS a ON a.id = da.application_id
		ORDER BY CASE da.state WHEN 'active' THEN 0 ELSE 1 END,
		         da.last_seen_at DESC, a.observed_host`)
	if err != nil {
		return DiscoveryResources{}, fmt.Errorf("query discovered application flows: %w", err)
	}
	defer featureRows.Close()
	for featureRows.Next() {
		var feature domain.DeviceApplication
		var applicationID, state, network, matchKind string
		var firstSeen, lastSeen int64
		var destinationPort int64
		if err := featureRows.Scan(
			&feature.ID, &feature.DeviceID, &applicationID, &feature.ObservedHost, &network,
			&feature.TransportHint, &feature.DestinationIP, &destinationPort, &state,
			&feature.ActiveConnections, &matchKind, &feature.Match.Value, &firstSeen, &lastSeen,
		); err != nil {
			return DiscoveryResources{}, fmt.Errorf("scan discovered application flow: %w", err)
		}
		feature.Network = domain.Network(network)
		feature.State = domain.ResourceState(state)
		feature.Match.Kind = domain.MatchKind(matchKind)
		feature.DestinationPort = uint16(destinationPort)
		feature.FirstSeen = time.Unix(firstSeen, 0).UTC()
		feature.LastSeen = time.Unix(lastSeen, 0).UTC()
		resources.DeviceApplications = append(resources.DeviceApplications, feature)
		if feature.LastSeen.After(resources.ConnectionsUpdatedAt) {
			resources.ConnectionsUpdatedAt = feature.LastSeen
		}
	}
	if err := featureRows.Err(); err != nil {
		return DiscoveryResources{}, fmt.Errorf("iterate discovered application flows: %w", err)
	}
	return resources, nil
}

func applyObservedFeatureTx(
	ctx context.Context,
	tx *sql.Tx,
	feature domain.ObservedFeature,
	fallbackNow time.Time,
) (deviceApplicationID, applicationID string, changed bool, err error) {
	if feature.ConnectionID == "" || feature.SourceIP == "" || feature.ObservedHost == "" {
		return "", "", false, fmt.Errorf("observed feature is missing connection id, source IP, or host")
	}
	observedAt := normalizeEventTime(feature.ObservedAt, fallbackNow)
	openedAt := normalizeEventTime(feature.OpenedAt, observedAt)
	deviceID, err := ensureObservedDeviceTx(ctx, tx, feature.SourceIP, observedAt)
	if err != nil {
		return "", "", false, err
	}
	applicationID, err = ensureApplicationTx(ctx, tx, feature.ObservedHost, observedAt)
	if err != nil {
		return "", "", false, err
	}
	if feature.Network == "" {
		feature.Network = domain.NetworkUnknown
	}
	deviceApplicationID = domain.StableID("da", deviceID+"|"+applicationID+"|"+string(feature.Network))
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO device_applications(
			id, device_id, application_id, network, transport_hint, destination_ip,
			destination_port, state, active_connections, first_seen_at, last_seen_at, inactive_at
		) VALUES(?, ?, ?, ?, ?, ?, ?, 'active', 0, ?, ?, NULL)
		ON CONFLICT(device_id, application_id, network) DO UPDATE SET
			transport_hint = CASE WHEN excluded.transport_hint <> '' THEN excluded.transport_hint ELSE device_applications.transport_hint END,
			destination_ip = CASE WHEN excluded.destination_ip <> '' THEN excluded.destination_ip ELSE device_applications.destination_ip END,
			destination_port = CASE WHEN excluded.destination_port <> 0 THEN excluded.destination_port ELSE device_applications.destination_port END,
			state = 'active',
			last_seen_at = excluded.last_seen_at,
			inactive_at = NULL`,
		deviceApplicationID, deviceID, applicationID, feature.Network, feature.TransportHint,
		feature.DestinationIP, feature.DestinationPort, observedAt.Unix(), observedAt.Unix(),
	); err != nil {
		return "", "", false, fmt.Errorf("upsert device application for %q: %w", feature.ConnectionID, err)
	}

	var previousDeviceApplicationID string
	var previousClosedAt sql.NullInt64
	previousFound := true
	if err := tx.QueryRowContext(ctx,
		"SELECT device_application_id, closed_at FROM connection_samples WHERE connection_id = ?", feature.ConnectionID,
	).Scan(&previousDeviceApplicationID, &previousClosedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			previousFound = false
		} else {
			return "", "", false, fmt.Errorf("load previous connection mapping: %w", err)
		}
	}
	changed = !previousFound || previousClosedAt.Valid || previousDeviceApplicationID != deviceApplicationID
	if previousDeviceApplicationID != "" && previousDeviceApplicationID != deviceApplicationID {
		// A connection ID should not normally change feature identity, but retaining this
		// affected combination prevents stale active counts if a controller reuses IDs.
		if err := reconcileDeviceApplicationTx(ctx, tx, previousDeviceApplicationID, observedAt); err != nil {
			return "", "", false, err
		}
	}

	proxyChain, err := json.Marshal(feature.ProxyChain)
	if err != nil {
		return "", "", false, fmt.Errorf("encode proxy chain for %q: %w", feature.ConnectionID, err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO connection_samples(
			connection_id, device_application_id, source_ip, destination_ip, observed_host, network,
			opened_at, last_observed_at, closed_at, upload_bytes, download_bytes, proxy_chain_json,
			matched_rule, matched_rule_payload
		) VALUES(?, ?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?, ?, ?)
		ON CONFLICT(connection_id) DO UPDATE SET
			device_application_id = excluded.device_application_id,
			source_ip = excluded.source_ip,
			destination_ip = excluded.destination_ip,
			observed_host = excluded.observed_host,
			network = excluded.network,
			last_observed_at = excluded.last_observed_at,
			closed_at = NULL,
			upload_bytes = excluded.upload_bytes,
			download_bytes = excluded.download_bytes,
			proxy_chain_json = excluded.proxy_chain_json,
			matched_rule = excluded.matched_rule,
			matched_rule_payload = excluded.matched_rule_payload`,
		feature.ConnectionID, deviceApplicationID, feature.SourceIP, nullIfEmpty(feature.DestinationIP),
		feature.ObservedHost, feature.Network, openedAt.Unix(), observedAt.Unix(), feature.UploadBytes,
		feature.DownloadBytes, string(proxyChain), nullIfEmpty(feature.MatchedRule),
		nullIfEmpty(feature.MatchedRulePayload),
	); err != nil {
		return "", "", false, fmt.Errorf("upsert connection sample %q: %w", feature.ConnectionID, err)
	}
	return deviceApplicationID, applicationID, changed, nil
}

func closeObservedFeatureTx(
	ctx context.Context,
	tx *sql.Tx,
	feature domain.ObservedFeature,
	fallbackNow time.Time,
) (deviceApplicationID, applicationID string, found bool, changed bool, err error) {
	if feature.ConnectionID == "" {
		return "", "", false, false, nil
	}
	if err := tx.QueryRowContext(ctx, `
		SELECT da.id, da.application_id
		FROM connection_samples AS cs
		JOIN device_applications AS da ON da.id = cs.device_application_id
		WHERE cs.connection_id = ?`, feature.ConnectionID,
	).Scan(&deviceApplicationID, &applicationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", false, false, nil
		}
		return "", "", false, false, fmt.Errorf("load connection sample for close: %w", err)
	}
	closedAt := normalizeEventTime(feature.ObservedAt, fallbackNow)
	result, err := tx.ExecContext(ctx, `
		UPDATE connection_samples
		SET closed_at = ?, last_observed_at = CASE WHEN last_observed_at < ? THEN ? ELSE last_observed_at END
		WHERE connection_id = ? AND closed_at IS NULL`,
		closedAt.Unix(), closedAt.Unix(), closedAt.Unix(), feature.ConnectionID,
	)
	if err != nil {
		return "", "", false, false, fmt.Errorf("close connection sample %q: %w", feature.ConnectionID, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return "", "", false, false, fmt.Errorf("read close result for %q: %w", feature.ConnectionID, err)
	}
	return deviceApplicationID, applicationID, true, rows > 0, nil
}

func ensureObservedDeviceTx(ctx context.Context, tx *sql.Tx, sourceIP string, observedAt time.Time) (string, error) {
	var existingID string
	err := tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE ip_address = ?", sourceIP).Scan(&existingID)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `
			UPDATE devices SET state = 'active', last_seen_at = ?, updated_at = ? WHERE id = ?`,
			observedAt.Unix(), observedAt.Unix(), existingID,
		); err != nil {
			return "", fmt.Errorf("refresh observed device %q: %w", sourceIP, err)
		}
		return existingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("lookup observed device %q: %w", sourceIP, err)
	}
	deviceID := domain.StableID("dev", sourceIP)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO devices(id, ip_address, mac_address, display_name, hostname, state, first_seen_at, last_seen_at, updated_at)
		VALUES(?, ?, NULL, ?, NULL, 'active', ?, ?, ?)`,
		deviceID, sourceIP, "设备 "+sourceIP, observedAt.Unix(), observedAt.Unix(), observedAt.Unix(),
	); err != nil {
		return "", fmt.Errorf("insert observed device %q: %w", sourceIP, err)
	}
	return deviceID, nil
}

func ensureApplicationTx(ctx context.Context, tx *sql.Tx, host string, observedAt time.Time) (string, error) {
	applicationID := domain.StableID("app", host)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO applications(id, observed_host, match_kind, match_value, state, first_seen_at, last_seen_at, updated_at)
		VALUES(?, ?, 'domain', ?, 'active', ?, ?, ?)
		ON CONFLICT(observed_host) DO UPDATE SET
			state = 'active', last_seen_at = excluded.last_seen_at, updated_at = excluded.updated_at`,
		applicationID, host, host, observedAt.Unix(), observedAt.Unix(), observedAt.Unix(),
	); err != nil {
		return "", fmt.Errorf("upsert observed application %q: %w", host, err)
	}
	var persistedID string
	if err := tx.QueryRowContext(ctx, "SELECT id FROM applications WHERE observed_host = ?", host).Scan(&persistedID); err != nil {
		return "", fmt.Errorf("resolve observed application %q: %w", host, err)
	}
	return persistedID, nil
}

func reconcileDeviceApplicationTx(ctx context.Context, tx *sql.Tx, deviceApplicationID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE device_applications
		SET active_connections = (
			SELECT COUNT(*) FROM connection_samples
			WHERE device_application_id = device_applications.id AND closed_at IS NULL
		),
		state = CASE WHEN EXISTS(
			SELECT 1 FROM connection_samples
			WHERE device_application_id = device_applications.id AND closed_at IS NULL
		) THEN 'active' ELSE 'inactive' END,
		inactive_at = CASE WHEN EXISTS(
			SELECT 1 FROM connection_samples
			WHERE device_application_id = device_applications.id AND closed_at IS NULL
		) THEN NULL ELSE ? END
		WHERE id = ?`, now.UTC().Unix(), deviceApplicationID,
	); err != nil {
		return fmt.Errorf("reconcile device application %q: %w", deviceApplicationID, err)
	}
	return nil
}

func reconcileApplicationTx(ctx context.Context, tx *sql.Tx, applicationID string, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		UPDATE applications
		SET state = CASE WHEN EXISTS(
			SELECT 1 FROM device_applications
			WHERE application_id = applications.id AND state = 'active'
		) THEN 'active' ELSE 'inactive' END,
		updated_at = ?
		WHERE id = ?`, now.UTC().Unix(), applicationID,
	); err != nil {
		return fmt.Errorf("reconcile application %q: %w", applicationID, err)
	}
	return nil
}

func normalizeEventTime(candidate, fallback time.Time) time.Time {
	if candidate.IsZero() {
		return fallback.UTC()
	}
	return candidate.UTC()
}

func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}
