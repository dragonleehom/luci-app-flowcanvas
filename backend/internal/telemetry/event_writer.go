package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
)

type FeatureEventApplier interface {
	ApplyFeatureEvents(ctx context.Context, events []domain.FeatureEvent) (store.FeatureApplyResult, error)
}

type FeatureWriterConfig struct {
	QueueCapacity int
	BatchSize     int
	FlushInterval time.Duration
}

type FeatureWriterStats struct {
	Queued      int
	Overflow    int
	Batches     uint64
	Applied     uint64
	ApplyErrors uint64
}

type FeatureWriter struct {
	applier FeatureEventApplier
	config  FeatureWriterConfig
	onApply func(store.FeatureApplyResult)

	queue chan domain.FeatureEvent

	overflowMu sync.Mutex
	overflow   map[string]domain.FeatureEvent

	queued      atomic.Int64
	batches     atomic.Uint64
	applied     atomic.Uint64
	applyErrors atomic.Uint64
}

func NewFeatureWriter(applier FeatureEventApplier, config FeatureWriterConfig, onApply func(store.FeatureApplyResult)) (*FeatureWriter, error) {
	if applier == nil {
		return nil, errors.New("feature writer requires a feature event applier")
	}
	if config.QueueCapacity <= 0 {
		config.QueueCapacity = 8192
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 256
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 200 * time.Millisecond
	}
	return &FeatureWriter{
		applier:  applier,
		config:   config,
		onApply:  onApply,
		queue:    make(chan domain.FeatureEvent, config.QueueCapacity),
		overflow: make(map[string]domain.FeatureEvent),
	}, nil
}

// ApplyFeatureEvents is intentionally non-blocking for the high-frequency Mihomo
// WebSocket loop. Once the bounded channel fills, it coalesces the newest event for
// each connection ID in an overflow map that the writer drains before each flush.
func (w *FeatureWriter) ApplyFeatureEvents(ctx context.Context, events []domain.FeatureEvent) error {
	for _, event := range events {
		if event.Feature.ConnectionID == "" {
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case w.queue <- event:
			w.queued.Add(1)
		default:
			w.overflowMu.Lock()
			w.overflow[event.Feature.ConnectionID] = event
			w.overflowMu.Unlock()
		}
	}
	return nil
}

func (w *FeatureWriter) Run(ctx context.Context) error {
	timer := time.NewTimer(w.config.FlushInterval)
	defer timer.Stop()
	batch := make([]domain.FeatureEvent, 0, w.config.BatchSize)

	flush := func() error {
		batch = append(batch, w.drainOverflow()...)
		if len(batch) == 0 {
			return nil
		}
		coalesced := coalesceEvents(batch)
		result, err := w.applier.ApplyFeatureEvents(ctx, coalesced)
		if err != nil {
			w.applyErrors.Add(1)
			return fmt.Errorf("apply feature event batch: %w", err)
		}
		w.batches.Add(1)
		w.applied.Add(uint64(len(coalesced)))
		if result.Changed && w.onApply != nil {
			w.onApply(result)
		}
		batch = batch[:0]
		return nil
	}

	for {
		select {
		case <-ctx.Done():
			// A bounded final flush preserves events already received before shutdown.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			batch = append(batch, drainChannel(w.queue)...)
			batch = append(batch, w.drainOverflow()...)
			if len(batch) > 0 {
				coalesced := coalesceEvents(batch)
				if _, err := w.applier.ApplyFeatureEvents(shutdownCtx, coalesced); err != nil {
					return fmt.Errorf("final feature event flush: %w", err)
				}
			}
			return nil
		case event := <-w.queue:
			batch = append(batch, event)
			w.queued.Add(-1)
			if len(batch) >= w.config.BatchSize {
				if err := flush(); err != nil {
					return err
				}
				resetTimer(timer, w.config.FlushInterval)
			}
		case <-timer.C:
			if err := flush(); err != nil {
				return err
			}
			resetTimer(timer, w.config.FlushInterval)
		}
	}
}

func (w *FeatureWriter) Stats() FeatureWriterStats {
	w.overflowMu.Lock()
	overflow := len(w.overflow)
	w.overflowMu.Unlock()
	return FeatureWriterStats{
		Queued:      int(w.queued.Load()),
		Overflow:    overflow,
		Batches:     w.batches.Load(),
		Applied:     w.applied.Load(),
		ApplyErrors: w.applyErrors.Load(),
	}
}

func (w *FeatureWriter) drainOverflow() []domain.FeatureEvent {
	w.overflowMu.Lock()
	defer w.overflowMu.Unlock()
	if len(w.overflow) == 0 {
		return nil
	}
	events := make([]domain.FeatureEvent, 0, len(w.overflow))
	for _, event := range w.overflow {
		events = append(events, event)
	}
	clear(w.overflow)
	return events
}

func coalesceEvents(events []domain.FeatureEvent) []domain.FeatureEvent {
	latest := make(map[string]domain.FeatureEvent, len(events))
	order := make([]string, 0, len(events))
	for _, event := range events {
		id := event.Feature.ConnectionID
		if id == "" {
			continue
		}
		if _, exists := latest[id]; !exists {
			order = append(order, id)
		}
		latest[id] = event
	}
	coalesced := make([]domain.FeatureEvent, 0, len(latest))
	for _, id := range order {
		coalesced = append(coalesced, latest[id])
	}
	return coalesced
}

func drainChannel(queue <-chan domain.FeatureEvent) []domain.FeatureEvent {
	events := make([]domain.FeatureEvent, 0)
	for {
		select {
		case event := <-queue:
			events = append(events, event)
		default:
			return events
		}
	}
}

func resetTimer(timer *time.Timer, duration time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(duration)
}
