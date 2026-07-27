package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

var ErrCompilationNotFound = errors.New("compilation revision not found")

func (s *Store) CreateCompilation(ctx context.Context, record domain.CompilationRecord) (domain.CompilationRecord, error) {
	if record.ID == "" {
		return domain.CompilationRecord{}, fmt.Errorf("compilation id is required")
	}
	if record.CanvasID == "" {
		return domain.CompilationRecord{}, fmt.Errorf("compilation canvas id is required")
	}
	if record.Status == "" {
		return domain.CompilationRecord{}, fmt.Errorf("compilation status is required")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	var appliedAt any
	if record.AppliedAt != nil {
		appliedAt = record.AppliedAt.UTC().Unix()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO compilation_revisions(
			id, canvas_id, canvas_revision, status, generated_yaml,
			mihomo_config_hash, error_message, created_at, applied_at
		) VALUES(?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, ''), ?, ?)`,
		record.ID, record.CanvasID, record.CanvasRevision, record.Status,
		record.ManagedYAML, record.ConfigHash, record.ErrorMessage,
		record.CreatedAt.UTC().Unix(), appliedAt,
	)
	if err != nil {
		return domain.CompilationRecord{}, fmt.Errorf("insert compilation audit record: %w", err)
	}
	return record, nil
}

func (s *Store) CompleteCompilation(
	ctx context.Context,
	id string,
	status domain.CompilationStatus,
	configHash, errorMessage string,
	appliedAt *time.Time,
) (domain.CompilationRecord, error) {
	var appliedValue any
	if appliedAt != nil {
		appliedValue = appliedAt.UTC().Unix()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE compilation_revisions
		SET status = ?, mihomo_config_hash = NULLIF(?, ''), error_message = NULLIF(?, ''), applied_at = ?
		WHERE id = ?`, status, configHash, errorMessage, appliedValue, id,
	)
	if err != nil {
		return domain.CompilationRecord{}, fmt.Errorf("complete compilation audit record: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.CompilationRecord{}, fmt.Errorf("read compilation update result: %w", err)
	}
	if rows == 0 {
		return domain.CompilationRecord{}, fmt.Errorf("%w: %s", ErrCompilationNotFound, id)
	}
	return s.GetCompilation(ctx, id)
}

func (s *Store) GetCompilation(ctx context.Context, id string) (domain.CompilationRecord, error) {
	var record domain.CompilationRecord
	var managedYAML, configHash, errorMessage sql.NullString
	var createdAt int64
	var appliedAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, canvas_id, canvas_revision, status, generated_yaml,
		       mihomo_config_hash, error_message, created_at, applied_at
		FROM compilation_revisions WHERE id = ?`, id,
	).Scan(
		&record.ID, &record.CanvasID, &record.CanvasRevision, &record.Status,
		&managedYAML, &configHash, &errorMessage, &createdAt, &appliedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CompilationRecord{}, fmt.Errorf("%w: %s", ErrCompilationNotFound, id)
	}
	if err != nil {
		return domain.CompilationRecord{}, fmt.Errorf("read compilation audit record: %w", err)
	}
	record.ManagedYAML = managedYAML.String
	record.ConfigHash = configHash.String
	record.ErrorMessage = errorMessage.String
	record.CreatedAt = time.Unix(createdAt, 0).UTC()
	if appliedAt.Valid {
		value := time.Unix(appliedAt.Int64, 0).UTC()
		record.AppliedAt = &value
	}
	return record, nil
}

func (s *Store) CreateCompilationRollback(ctx context.Context, rollback domain.CompilationRollback) (domain.CompilationRollback, error) {
	if rollback.ID == "" || rollback.CompilationID == "" {
		return domain.CompilationRollback{}, fmt.Errorf("rollback id and compilation id are required")
	}
	if rollback.CreatedAt.IsZero() {
		rollback.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO compilation_rollbacks(
			id, compilation_id, prior_config_hash, candidate_config_hash, backup_path,
			status, error_message, created_at, restored_at
		) VALUES(?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, NULL)`,
		rollback.ID, rollback.CompilationID, rollback.PriorConfigHash, rollback.CandidateConfigHash,
		rollback.BackupPath, rollback.Status, rollback.ErrorMessage, rollback.CreatedAt.UTC().Unix(),
	)
	if err != nil {
		return domain.CompilationRollback{}, fmt.Errorf("insert compilation rollback audit: %w", err)
	}
	return rollback, nil
}

func (s *Store) CompleteCompilationRollback(
	ctx context.Context,
	compilationID string,
	status domain.RollbackStatus,
	errorMessage string,
	restoredAt *time.Time,
) (domain.CompilationRollback, error) {
	var restoredValue any
	if restoredAt != nil {
		restoredValue = restoredAt.UTC().Unix()
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE compilation_rollbacks
		SET status = ?, error_message = NULLIF(?, ''), restored_at = ?
		WHERE compilation_id = ?`, status, errorMessage, restoredValue, compilationID,
	)
	if err != nil {
		return domain.CompilationRollback{}, fmt.Errorf("complete compilation rollback audit: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return domain.CompilationRollback{}, fmt.Errorf("read rollback update result: %w", err)
	}
	if rows == 0 {
		return domain.CompilationRollback{}, fmt.Errorf("rollback audit missing for compilation %s", compilationID)
	}
	return s.GetCompilationRollback(ctx, compilationID)
}

func (s *Store) GetCompilationRollback(ctx context.Context, compilationID string) (domain.CompilationRollback, error) {
	var rollback domain.CompilationRollback
	var errorMessage sql.NullString
	var createdAt int64
	var restoredAt sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, compilation_id, prior_config_hash, candidate_config_hash, backup_path,
		       status, error_message, created_at, restored_at
		FROM compilation_rollbacks WHERE compilation_id = ?`, compilationID,
	).Scan(
		&rollback.ID, &rollback.CompilationID, &rollback.PriorConfigHash, &rollback.CandidateConfigHash,
		&rollback.BackupPath, &rollback.Status, &errorMessage, &createdAt, &restoredAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.CompilationRollback{}, sql.ErrNoRows
	}
	if err != nil {
		return domain.CompilationRollback{}, fmt.Errorf("read compilation rollback audit: %w", err)
	}
	rollback.ErrorMessage = errorMessage.String
	rollback.CreatedAt = time.Unix(createdAt, 0).UTC()
	if restoredAt.Valid {
		value := time.Unix(restoredAt.Int64, 0).UTC()
		rollback.RestoredAt = &value
	}
	return rollback, nil
}
