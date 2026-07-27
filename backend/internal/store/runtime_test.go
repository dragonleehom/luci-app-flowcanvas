package store

import (
	"context"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestApplyFeatureEventsSynchronizesActiveAndHistoricalState(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()

	startedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	observedAt := startedAt.Add(10 * time.Second)
	first := observedFeature("connection-a", startedAt, observedAt)
	second := observedFeature("connection-b", startedAt.Add(time.Second), observedAt)

	result, err := database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{
		{Kind: domain.FeatureObserved, Feature: first},
		{Kind: domain.FeatureObserved, Feature: second},
	})
	if err != nil {
		t.Fatalf("apply observed events: %v", err)
	}
	if !result.Changed || result.Observed != 2 {
		t.Fatalf("unexpected observed result: %+v", result)
	}
	assertSingleFeature(t, database, domain.StateActive, 2)

	first.ObservedAt = observedAt.Add(5 * time.Second)
	result, err = database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{
		{Kind: domain.FeatureClosed, Feature: first},
	})
	if err != nil {
		t.Fatalf("close first connection: %v", err)
	}
	if !result.Changed || result.Closed != 1 {
		t.Fatalf("unexpected first close result: %+v", result)
	}
	assertSingleFeature(t, database, domain.StateActive, 1)

	second.ObservedAt = observedAt.Add(8 * time.Second)
	result, err = database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{
		{Kind: domain.FeatureClosed, Feature: second},
	})
	if err != nil {
		t.Fatalf("close second connection: %v", err)
	}
	if !result.Changed || result.Closed != 1 {
		t.Fatalf("unexpected second close result: %+v", result)
	}
	assertSingleFeature(t, database, domain.StateInactive, 0)

	var connectionCount, closedCount int
	if err := database.db.QueryRowContext(ctx, `
		SELECT COUNT(*), COUNT(closed_at) FROM connection_samples`,
	).Scan(&connectionCount, &closedCount); err != nil {
		t.Fatalf("query historical connections: %v", err)
	}
	if connectionCount != 2 || closedCount != 2 {
		t.Fatalf("expected historical closed samples, got total=%d closed=%d", connectionCount, closedCount)
	}

	first.ObservedAt = observedAt.Add(20 * time.Second)
	result, err = database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{
		{Kind: domain.FeatureObserved, Feature: first},
	})
	if err != nil {
		t.Fatalf("re-observe connection: %v", err)
	}
	if !result.Changed || result.Observed != 1 {
		t.Fatalf("unexpected reopen result: %+v", result)
	}
	assertSingleFeature(t, database, domain.StateActive, 1)
}

func assertSingleFeature(t *testing.T, database *Store, state domain.ResourceState, activeConnections int) {
	t.Helper()
	resources, err := database.LoadDiscoveryResources(context.Background())
	if err != nil {
		t.Fatalf("load discovery resources: %v", err)
	}
	if len(resources.Devices) != 1 {
		t.Fatalf("expected one observed device, got %d", len(resources.Devices))
	}
	if len(resources.DeviceApplications) != 1 {
		t.Fatalf("expected one observed feature, got %d", len(resources.DeviceApplications))
	}
	feature := resources.DeviceApplications[0]
	if feature.State != state || feature.ActiveConnections != activeConnections {
		t.Fatalf("unexpected feature state: %+v", feature)
	}
}

func observedFeature(connectionID string, openedAt, observedAt time.Time) domain.ObservedFeature {
	return domain.ObservedFeature{
		ConnectionID:       connectionID,
		SourceIP:           "192.168.1.50",
		DestinationIP:      "203.0.113.8",
		DestinationPort:    443,
		ObservedHost:       "video.example.com",
		Network:            domain.NetworkQUIC,
		TransportHint:      "quic",
		OpenedAt:           openedAt,
		ObservedAt:         observedAt,
		UploadBytes:        1024,
		DownloadBytes:      2048,
		ProxyChain:         []string{"Proxy-A"},
		MatchedRule:        "MATCH",
		MatchedRulePayload: "",
	}
}

func TestApplyFeatureEventsMarksOnlyLifecycleTransitionsChanged(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()

	now := time.Date(2026, 7, 27, 14, 0, 0, 0, time.UTC)
	feature := observedFeature("lifecycle-connection", now, now)
	result, err := database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{{Kind: domain.FeatureObserved, Feature: feature}})
	if err != nil || !result.Changed {
		t.Fatalf("expected initial observed state change, result=%+v err=%v", result, err)
	}

	feature.ObservedAt = now.Add(time.Second)
	feature.DownloadBytes = 4096
	result, err = database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{{Kind: domain.FeatureObserved, Feature: feature}})
	if err != nil || result.Changed {
		t.Fatalf("expected steady observed refresh without canvas state change, result=%+v err=%v", result, err)
	}

	feature.ObservedAt = now.Add(2 * time.Second)
	result, err = database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{{Kind: domain.FeatureClosed, Feature: feature}})
	if err != nil || !result.Changed {
		t.Fatalf("expected first close to change canvas state, result=%+v err=%v", result, err)
	}

	feature.ObservedAt = now.Add(3 * time.Second)
	result, err = database.ApplyFeatureEvents(ctx, []domain.FeatureEvent{{Kind: domain.FeatureClosed, Feature: feature}})
	if err != nil || result.Changed {
		t.Fatalf("expected repeated close without canvas state change, result=%+v err=%v", result, err)
	}
}
