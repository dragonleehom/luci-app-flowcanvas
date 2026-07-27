package store

import (
	"context"
	"testing"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

func TestCompilationAndRollbackAuditLifecycle(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer database.Close()

	createdAt := time.Date(2026, 7, 27, 16, 0, 0, 0, time.UTC)
	record, err := database.CreateCompilation(ctx, domain.CompilationRecord{
		ID: "compile-1", CanvasID: "default", CanvasRevision: 8,
		Status: domain.CompilationValidated, ManagedYAML: "rules: []\n", ConfigHash: "overlay-hash", CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create compilation: %v", err)
	}
	if record.Status != domain.CompilationValidated || record.ID != "compile-1" {
		t.Fatalf("unexpected created record: %+v", record)
	}

	rollback, err := database.CreateCompilationRollback(ctx, domain.CompilationRollback{
		ID: "rollback-1", CompilationID: record.ID, PriorConfigHash: "prior-hash", CandidateConfigHash: "candidate-hash",
		BackupPath: "/etc/mihomo/.flowcanvas-backups/compile-1.yaml", Status: domain.RollbackNotNeeded, CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatalf("create rollback audit: %v", err)
	}
	if rollback.Status != domain.RollbackNotNeeded {
		t.Fatalf("unexpected rollback record: %+v", rollback)
	}

	appliedAt := createdAt.Add(time.Second)
	completed, err := database.CompleteCompilation(ctx, record.ID, domain.CompilationApplied, "candidate-hash", "", &appliedAt)
	if err != nil {
		t.Fatalf("complete compilation: %v", err)
	}
	if completed.Status != domain.CompilationApplied || completed.ConfigHash != "candidate-hash" || completed.AppliedAt == nil {
		t.Fatalf("unexpected completed record: %+v", completed)
	}

	restoredAt := appliedAt.Add(time.Second)
	completedRollback, err := database.CompleteCompilationRollback(ctx, record.ID, domain.RollbackRestored, "reload rejected candidate", &restoredAt)
	if err != nil {
		t.Fatalf("complete rollback: %v", err)
	}
	if completedRollback.Status != domain.RollbackRestored || completedRollback.ErrorMessage == "" || completedRollback.RestoredAt == nil {
		t.Fatalf("unexpected completed rollback: %+v", completedRollback)
	}

	var migrationCount int
	if err := database.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_migrations WHERE version = 2").Scan(&migrationCount); err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if migrationCount != 1 {
		t.Fatalf("expected compilation migration version 2, got %d", migrationCount)
	}
}
