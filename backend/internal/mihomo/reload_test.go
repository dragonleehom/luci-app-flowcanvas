package mihomo

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReloadConfigUsesForceAndAuthentication(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/configs" || r.URL.Query().Get("force") != "true" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		var request ConfigReloadRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if request.Path != "/etc/mihomo/config.yaml" || request.Payload != "" {
			http.Error(w, "unexpected payload", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(server.URL, "test-secret", server.Client())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := client.ReloadConfig(context.Background(), "/etc/mihomo/config.yaml", ""); err != nil {
		t.Fatalf("reload config: %v", err)
	}
}

func TestReloadConfigRejectsNonNoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid YAML", http.StatusBadRequest)
	}))
	defer server.Close()
	client, err := NewClient(server.URL, "", server.Client())
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	err = client.ReloadConfig(context.Background(), "/etc/mihomo/config.yaml", "")
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("expected detailed reload error, got %v", err)
	}
}
