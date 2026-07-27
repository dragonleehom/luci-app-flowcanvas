package compiler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
)

func TestApplyWritesCandidateReloadsAndAuditsSuccess(t *testing.T) {
	original := []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n")
	service, database, configPath, backupDirectory, controller := newTestService(t, original, nil, nil)
	defer database.Close()
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	})

	result, err := service.Apply(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("apply compilation: %v", err)
	}
	if result.Compilation.Status != domain.CompilationApplied || result.Rollback == nil || result.Rollback.Status != domain.RollbackNotNeeded {
		t.Fatalf("unexpected successful apply result: %+v", result)
	}
	if len(controller.reloadPaths) != 1 || controller.reloadPaths[0] != configPath || controller.proxyCalls != 1 {
		t.Fatalf("unexpected controller calls: %+v proxyCalls=%d", controller.reloadPaths, controller.proxyCalls)
	}
	candidate, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read candidate config: %v", err)
	}
	if !strings.Contains(string(candidate), "flowcanvas-") || !strings.Contains(string(candidate), "RULE-SET,") {
		t.Fatalf("candidate did not contain managed config: %s", candidate)
	}
	backup, err := os.ReadFile(filepath.Join(backupDirectory, result.Compilation.ID+".yaml"))
	if err != nil {
		t.Fatalf("read backup config: %v", err)
	}
	if string(backup) != string(original) {
		t.Fatalf("backup did not preserve original:\nwant %s\ngot %s", original, backup)
	}
}

func TestApplyRestoresOriginalAfterMihomoRejectsCandidate(t *testing.T) {
	original := []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n")
	service, database, configPath, _, controller := newTestService(t, original, []error{errors.New("invalid candidate"), nil}, nil)
	defer database.Close()
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	})

	result, err := service.Apply(context.Background(), snapshot)
	if err == nil {
		t.Fatal("expected failed candidate apply")
	}
	if result.Compilation.Status != domain.CompilationRolledBack || result.Rollback == nil || result.Rollback.Status != domain.RollbackRestored {
		t.Fatalf("unexpected rollback result: %+v", result)
	}
	if len(controller.reloadPaths) != 2 {
		t.Fatalf("expected candidate and rollback reloads, got %+v", controller.reloadPaths)
	}
	restored, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read restored config: %v", err)
	}
	if string(restored) != string(original) {
		t.Fatalf("original config was not restored:\nwant %s\ngot %s", original, restored)
	}
}

func TestApplyMarksRollbackFailureWhenRestoreReloadFails(t *testing.T) {
	original := []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n")
	service, database, _, _, _ := newTestService(t, original, []error{errors.New("candidate rejected"), errors.New("rollback rejected")}, nil)
	defer database.Close()
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	})
	result, err := service.Apply(context.Background(), snapshot)
	if err == nil {
		t.Fatal("expected rollback failure")
	}
	if result.Compilation.Status != domain.CompilationFailed || result.Rollback == nil || result.Rollback.Status != domain.RollbackFailed {
		t.Fatalf("unexpected rollback failure result: %+v", result)
	}
}

func TestValidateDoesNotWriteMihomoConfig(t *testing.T) {
	original := []byte("mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n")
	service, database, configPath, _, controller := newTestService(t, original, nil, nil)
	defer database.Close()
	snapshot := compilationSnapshot([]domain.CanvasEdge{
		{ID: "s-f", Source: "source:tv", Target: "filter:domain", Kind: domain.EdgeSourceToFilter},
		{ID: "f-t", Source: "filter:domain", Target: "target:us", Kind: domain.EdgeFilterToTarget},
	})
	result, err := service.Validate(context.Background(), snapshot)
	if err != nil {
		t.Fatalf("validate compilation: %v", err)
	}
	if result.Compilation.Status != domain.CompilationValidated || len(controller.reloadPaths) != 0 {
		t.Fatalf("unexpected validation result: %+v", result)
	}
	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(contents) != string(original) {
		t.Fatalf("validate modified config:\n%s", contents)
	}
}

func newTestService(t *testing.T, config []byte, reloadErrors []error, proxyError error) (*Service, *store.Store, string, string, *testController) {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(configPath, config, 0o640); err != nil {
		t.Fatalf("write config fixture: %v", err)
	}
	database, err := store.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	controller := &testController{reloadErrors: reloadErrors, proxyError: proxyError}
	service, err := NewService(database, controller, ApplyOptions{
		ConfigPath:      configPath,
		BackupDirectory: filepath.Join(directory, "backups"),
		Timeout:         2 * time.Second,
	})
	if err != nil {
		database.Close()
		t.Fatalf("create service: %v", err)
	}
	return service, database, configPath, filepath.Join(directory, "backups"), controller
}

type testController struct {
	reloadErrors []error
	reloadPaths  []string
	proxyError   error
	proxyCalls   int
}

func (c *testController) ReloadConfig(_ context.Context, path, _ string) error {
	c.reloadPaths = append(c.reloadPaths, path)
	index := len(c.reloadPaths) - 1
	if index < len(c.reloadErrors) {
		return c.reloadErrors[index]
	}
	return nil
}

func (c *testController) Proxies(_ context.Context) (mihomo.ProxyCatalogResponse, error) {
	c.proxyCalls++
	return mihomo.ProxyCatalogResponse{}, c.proxyError
}
