package telemetry

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
)

func TestCoalesceEventsKeepsLatestStateForConnection(t *testing.T) {
	startedAt := time.Now().UTC()
	coalesced := coalesceEvents([]domain.FeatureEvent{
		{Kind: domain.FeatureObserved, Feature: domain.ObservedFeature{ConnectionID: "same", UploadBytes: 1, ObservedAt: startedAt}},
		{Kind: domain.FeatureObserved, Feature: domain.ObservedFeature{ConnectionID: "other", UploadBytes: 2, ObservedAt: startedAt}},
		{Kind: domain.FeatureClosed, Feature: domain.ObservedFeature{ConnectionID: "same", UploadBytes: 3, ObservedAt: startedAt.Add(time.Second)}},
	})
	if len(coalesced) != 2 {
		t.Fatalf("expected 2 coalesced events, got %d", len(coalesced))
	}
	if coalesced[0].Feature.ConnectionID != "same" || coalesced[0].Kind != domain.FeatureClosed || coalesced[0].Feature.UploadBytes != 3 {
		t.Fatalf("expected latest same connection event, got %+v", coalesced[0])
	}
}

func TestFeatureWriterFlushesBatch(t *testing.T) {
	recorder := &eventRecorder{received: make(chan []domain.FeatureEvent, 1)}
	writer, err := NewFeatureWriter(recorder, FeatureWriterConfig{
		QueueCapacity: 128,
		BatchSize:     8,
		FlushInterval: 10 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("create writer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()

	now := time.Now().UTC()
	if err := writer.ApplyFeatureEvents(context.Background(), []domain.FeatureEvent{
		{Kind: domain.FeatureObserved, Feature: domain.ObservedFeature{ConnectionID: "a", ObservedAt: now}},
		{Kind: domain.FeatureObserved, Feature: domain.ObservedFeature{ConnectionID: "b", ObservedAt: now}},
	}); err != nil {
		t.Fatalf("enqueue events: %v", err)
	}
	select {
	case events := <-recorder.received:
		if len(events) != 2 {
			t.Fatalf("expected 2 flushed events, got %d", len(events))
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for feature batch")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("writer shutdown: %v", err)
	}
}

type eventRecorder struct {
	mu       sync.Mutex
	received chan []domain.FeatureEvent
}

func (r *eventRecorder) ApplyFeatureEvents(_ context.Context, events []domain.FeatureEvent) (store.FeatureApplyResult, error) {
	copyEvents := append([]domain.FeatureEvent(nil), events...)
	r.mu.Lock()
	r.received <- copyEvents
	r.mu.Unlock()
	return store.FeatureApplyResult{Changed: true, Observed: len(events)}, nil
}
