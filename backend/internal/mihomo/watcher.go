package mihomo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

type FeatureEventKind string

const (
	FeatureObserved FeatureEventKind = "observed"
	FeatureClosed   FeatureEventKind = "closed"
)

type FeatureEvent struct {
	Kind    FeatureEventKind
	Feature domain.ObservedFeature
}

type FeatureSink interface {
	ApplyFeatureEvents(ctx context.Context, events []FeatureEvent) error
}

type Watcher struct {
	client     *Client
	sink       FeatureSink
	interval   time.Duration
	mu         sync.Mutex
	activeByID map[string]domain.ObservedFeature
}

func NewWatcher(client *Client, sink FeatureSink, interval time.Duration) *Watcher {
	return &Watcher{
		client:     client,
		sink:       sink,
		interval:   interval,
		activeByID: make(map[string]domain.ObservedFeature),
	}
}

func (w *Watcher) Run(ctx context.Context) error {
	if w.client == nil || w.sink == nil {
		return errors.New("mihomo watcher requires both client and feature sink")
	}
	backoff := 250 * time.Millisecond
	for {
		err := w.client.StreamConnections(ctx, w.interval, w.handleSnapshot)
		if ctx.Err() != nil {
			return nil
		}
		if err == nil {
			backoff = 250 * time.Millisecond
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (w *Watcher) handleSnapshot(snapshot ConnectionSnapshot) error {
	now := time.Now().UTC()
	current := make(map[string]domain.ObservedFeature, len(snapshot.Connections))
	for _, connection := range snapshot.Connections {
		feature, ok := NormalizeConnection(connection, now)
		if !ok {
			continue
		}
		current[feature.ConnectionID] = feature
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	events := make([]FeatureEvent, 0, len(current)+len(w.activeByID))
	for connectionID, feature := range current {
		events = append(events, FeatureEvent{Kind: FeatureObserved, Feature: feature})
		w.activeByID[connectionID] = feature
	}
	for connectionID, feature := range w.activeByID {
		if _, stillActive := current[connectionID]; stillActive {
			continue
		}
		feature.ObservedAt = now
		events = append(events, FeatureEvent{Kind: FeatureClosed, Feature: feature})
		delete(w.activeByID, connectionID)
	}
	if len(events) == 0 {
		return nil
	}
	if err := w.sink.ApplyFeatureEvents(context.Background(), events); err != nil {
		return fmt.Errorf("apply feature events: %w", err)
	}
	return nil
}
