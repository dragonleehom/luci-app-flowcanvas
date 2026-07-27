package mihomo

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestWatcherDiffsObservedAndClosedConnections(t *testing.T) {
	sink := &recordingFeatureSink{}
	watcher := NewWatcher(nil, sink, time.Second)
	startedAt := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)

	if err := watcher.handleSnapshot(ConnectionSnapshot{Connections: []Connection{{
		ID:    "connection-a",
		Start: startedAt,
		Metadata: Metadata{
			SourceIP:      "192.168.1.50",
			DestinationIP: "203.0.113.10",
			Host:          "v.qq.com",
			Network:       "tcp",
		},
	}}}); err != nil {
		t.Fatalf("handle observed snapshot: %v", err)
	}
	if err := watcher.handleSnapshot(ConnectionSnapshot{}); err != nil {
		t.Fatalf("handle closed snapshot: %v", err)
	}

	batches := sink.Batches()
	if len(batches) != 2 {
		t.Fatalf("expected observed and closed batches, got %d", len(batches))
	}
	if len(batches[0]) != 1 || batches[0][0].Kind != domain.FeatureObserved {
		t.Fatalf("unexpected observed batch: %+v", batches[0])
	}
	if len(batches[1]) != 1 || batches[1][0].Kind != domain.FeatureClosed {
		t.Fatalf("unexpected closed batch: %+v", batches[1])
	}
	if batches[1][0].Feature.ConnectionID != "connection-a" || batches[1][0].Feature.ObservedHost != "v.qq.com" {
		t.Fatalf("closed event lost original feature identity: %+v", batches[1][0])
	}
}

type recordingFeatureSink struct {
	mu      sync.Mutex
	batches [][]domain.FeatureEvent
}

func (s *recordingFeatureSink) ApplyFeatureEvents(_ context.Context, events []domain.FeatureEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches = append(s.batches, append([]domain.FeatureEvent(nil), events...))
	return nil
}

func (s *recordingFeatureSink) Batches() [][]domain.FeatureEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([][]domain.FeatureEvent, len(s.batches))
	for index, batch := range s.batches {
		result[index] = append([]domain.FeatureEvent(nil), batch...)
	}
	return result
}
