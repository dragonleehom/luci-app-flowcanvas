package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
	"nhooyr.io/websocket"
)

func TestRealtimeMihomoSnapshotPersistsThenBecomesHistorical(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/connections" {
			http.NotFound(w, r)
			return
		}
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close(websocket.StatusNormalClosure, "test complete")
		startedAt := time.Now().UTC().Add(-time.Second)
		observed := mihomo.ConnectionSnapshot{Connections: []mihomo.Connection{{
			ID:    "integration-connection",
			Start: startedAt,
			Metadata: mihomo.Metadata{
				SourceIP:        "192.168.1.50",
				DestinationIP:   "203.0.113.10",
				DestinationPort: 443,
				SniffHost:       "v.qq.com",
				Network:         "tcp",
				SniffType:       "tls",
			},
			Upload:   123,
			Download: 456,
		}}}
		writeSnapshot(t, r.Context(), connection, observed)
		time.Sleep(25 * time.Millisecond)
		writeSnapshot(t, r.Context(), connection, mihomo.ConnectionSnapshot{})
		<-r.Context().Done()
	}))
	defer server.Close()

	database, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	writer, err := telemetry.NewFeatureWriter(database, telemetry.FeatureWriterConfig{
		QueueCapacity: 128,
		BatchSize:     1,
		FlushInterval: 5 * time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("create feature writer: %v", err)
	}
	client, err := mihomo.NewClient(server.URL, "", nil)
	if err != nil {
		t.Fatalf("create Mihomo client: %v", err)
	}
	watcher := mihomo.NewWatcher(client, writer, 5*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writerDone := make(chan error, 1)
	watcherDone := make(chan error, 1)
	go func() { writerDone <- writer.Run(ctx) }()
	go func() { watcherDone <- watcher.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		resources, err := database.LoadDiscoveryResources(context.Background())
		if err != nil {
			t.Fatalf("load discovery resources: %v", err)
		}
		if len(resources.DeviceApplications) == 1 {
			feature := resources.DeviceApplications[0]
			if feature.ObservedHost != "v.qq.com" {
				t.Fatalf("unexpected observed host: %+v", feature)
			}
			if feature.State == domain.StateInactive && feature.ActiveConnections == 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for WebSocket snapshots to persist")
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	if err := <-writerDone; err != nil {
		t.Fatalf("writer stopped with error: %v", err)
	}
	if err := <-watcherDone; err != nil {
		t.Fatalf("watcher stopped with error: %v", err)
	}
}

func writeSnapshot(t *testing.T, ctx context.Context, connection *websocket.Conn, snapshot mihomo.ConnectionSnapshot) {
	t.Helper()
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if err := connection.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatalf("write snapshot: %v", err)
	}
}
