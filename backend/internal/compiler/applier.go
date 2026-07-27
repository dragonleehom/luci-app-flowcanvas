package compiler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/mihomo"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
)

const maxConfigurationBytes = 16 << 20

type AuditStore interface {
	CreateCompilation(ctx context.Context, record domain.CompilationRecord) (domain.CompilationRecord, error)
	CompleteCompilation(ctx context.Context, id string, status domain.CompilationStatus, configHash, errorMessage string, appliedAt *time.Time) (domain.CompilationRecord, error)
	CreateCompilationRollback(ctx context.Context, rollback domain.CompilationRollback) (domain.CompilationRollback, error)
	CompleteCompilationRollback(ctx context.Context, compilationID string, status domain.RollbackStatus, errorMessage string, restoredAt *time.Time) (domain.CompilationRollback, error)
}

type Controller interface {
	ReloadConfig(ctx context.Context, path, payload string) error
	Proxies(ctx context.Context) (mihomo.ProxyCatalogResponse, error)
}

type ApplyOptions struct {
	ConfigPath      string
	BackupDirectory string
	Timeout         time.Duration
}

type Service struct {
	audit      AuditStore
	controller Controller
	options    ApplyOptions
	applyMu    sync.Mutex
}

type ExecutionError struct {
	CompilationID string
	Err           error
}

func (e *ExecutionError) Error() string { return e.Err.Error() }
func (e *ExecutionError) Unwrap() error { return e.Err }

func NewService(audit AuditStore, controller Controller, options ApplyOptions) (*Service, error) {
	if audit == nil {
		return nil, errors.New("compilation service requires an audit store")
	}
	if controller == nil {
		return nil, errors.New("compilation service requires a Mihomo controller")
	}
	if options.ConfigPath == "" || !filepath.IsAbs(options.ConfigPath) {
		return nil, errors.New("compilation service requires an absolute Mihomo config path")
	}
	if options.BackupDirectory == "" || !filepath.IsAbs(options.BackupDirectory) {
		return nil, errors.New("compilation service requires an absolute backup directory")
	}
	if options.Timeout <= 0 {
		options.Timeout = 15 * time.Second
	}
	return &Service{audit: audit, controller: controller, options: options}, nil
}

// Validate compiles the current canvas without touching the Mihomo configuration
// file or invoking the controller. A successful preview is still audited.
func (s *Service) Validate(ctx context.Context, snapshot domain.CanvasSnapshot) (domain.CompilationResult, error) {
	preview, err := Compile(snapshot)
	if err != nil {
		record := s.recordFailure(ctx, snapshot, "", err)
		return domain.CompilationResult{Compilation: record}, &ExecutionError{CompilationID: record.ID, Err: err}
	}
	record, err := s.createRecord(ctx, snapshot, preview, domain.CompilationValidated)
	if err != nil {
		return domain.CompilationResult{}, err
	}
	return domain.CompilationResult{Compilation: record, Preview: preview}, nil
}

// Apply merges the compiled overlay into the configured Mihomo main YAML and
// reloads it. It serializes apply operations so concurrent browser requests
// cannot interleave backups or make a rollback restore another request's file.
func (s *Service) Apply(ctx context.Context, snapshot domain.CanvasSnapshot) (domain.CompilationResult, error) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	preview, err := Compile(snapshot)
	if err != nil {
		record := s.recordFailure(ctx, snapshot, "", err)
		return domain.CompilationResult{Compilation: record}, &ExecutionError{CompilationID: record.ID, Err: err}
	}
	record, err := s.createRecord(ctx, snapshot, preview, domain.CompilationDraft)
	if err != nil {
		return domain.CompilationResult{}, err
	}
	result := domain.CompilationResult{Compilation: record, Preview: preview}

	operationCtx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	original, mode, err := readRegularFile(s.options.ConfigPath)
	if err != nil {
		return s.failWithoutRollback(operationCtx, result, err)
	}
	candidate, err := MergeManagedOverlay(original, preview)
	if err != nil {
		return s.failWithoutRollback(operationCtx, result, err)
	}
	priorHash := contentHash(original)
	candidateHash := contentHash(candidate)
	backupPath := filepath.Join(s.options.BackupDirectory, record.ID+".yaml")
	if err := atomicWriteFile(backupPath, original, mode); err != nil {
		return s.failWithoutRollback(operationCtx, result, fmt.Errorf("write config backup: %w", err))
	}
	rollbackID, err := domain.NewAuditID("rollback")
	if err != nil {
		return s.failWithoutRollback(operationCtx, result, fmt.Errorf("generate rollback audit id: %w", err))
	}
	rollback, err := s.audit.CreateCompilationRollback(operationCtx, domain.CompilationRollback{
		ID:                  rollbackID,
		CompilationID:       record.ID,
		PriorConfigHash:     priorHash,
		CandidateConfigHash: candidateHash,
		BackupPath:          backupPath,
		Status:              domain.RollbackNotNeeded,
		CreatedAt:           time.Now().UTC(),
	})
	if err != nil {
		return s.failWithoutRollback(operationCtx, result, fmt.Errorf("create rollback audit: %w", err))
	}
	result.Rollback = &rollback
	if err := atomicWriteFile(s.options.ConfigPath, candidate, mode); err != nil {
		return s.failWithRollback(operationCtx, result, original, mode, candidateHash, fmt.Errorf("write candidate Mihomo config: %w", err))
	}
	if err := s.controller.ReloadConfig(operationCtx, s.options.ConfigPath, ""); err != nil {
		return s.failWithRollback(operationCtx, result, original, mode, candidateHash, fmt.Errorf("Mihomo rejected candidate configuration: %w", err))
	}
	if _, err := s.controller.Proxies(operationCtx); err != nil {
		return s.failWithRollback(operationCtx, result, original, mode, candidateHash, fmt.Errorf("Mihomo post-reload health probe failed: %w", err))
	}
	appliedAt := time.Now().UTC()
	completed, err := s.audit.CompleteCompilation(operationCtx, record.ID, domain.CompilationApplied, candidateHash, "", &appliedAt)
	if err != nil {
		return domain.CompilationResult{}, fmt.Errorf("complete applied compilation audit: %w", err)
	}
	result.Compilation = completed
	return result, nil
}

func (s *Service) createRecord(ctx context.Context, snapshot domain.CanvasSnapshot, preview domain.CompilationPreview, status domain.CompilationStatus) (domain.CompilationRecord, error) {
	id, err := domain.NewAuditID("compile")
	if err != nil {
		return domain.CompilationRecord{}, fmt.Errorf("generate compilation audit id: %w", err)
	}
	return s.audit.CreateCompilation(ctx, domain.CompilationRecord{
		ID:             id,
		CanvasID:       snapshot.Canvas.ID,
		CanvasRevision: snapshot.Canvas.Revision,
		Status:         status,
		ManagedYAML:    preview.ManagedYAML,
		ConfigHash:     preview.ContentHash,
		CreatedAt:      time.Now().UTC(),
	})
}

func (s *Service) recordFailure(ctx context.Context, snapshot domain.CanvasSnapshot, managedYAML string, cause error) domain.CompilationRecord {
	id, idErr := domain.NewAuditID("compile")
	if idErr != nil {
		return domain.CompilationRecord{CanvasID: snapshot.Canvas.ID, CanvasRevision: snapshot.Canvas.Revision, Status: domain.CompilationFailed, ErrorMessage: cause.Error()}
	}
	record, err := s.audit.CreateCompilation(ctx, domain.CompilationRecord{
		ID:             id,
		CanvasID:       snapshot.Canvas.ID,
		CanvasRevision: snapshot.Canvas.Revision,
		Status:         domain.CompilationFailed,
		ManagedYAML:    managedYAML,
		ErrorMessage:   safeErrorMessage(cause),
		CreatedAt:      time.Now().UTC(),
	})
	if err != nil {
		return domain.CompilationRecord{ID: id, CanvasID: snapshot.Canvas.ID, CanvasRevision: snapshot.Canvas.Revision, Status: domain.CompilationFailed, ErrorMessage: safeErrorMessage(cause)}
	}
	return record
}

func (s *Service) failWithoutRollback(ctx context.Context, result domain.CompilationResult, cause error) (domain.CompilationResult, error) {
	completed, err := s.audit.CompleteCompilation(ctx, result.Compilation.ID, domain.CompilationFailed, result.Compilation.ConfigHash, safeErrorMessage(cause), nil)
	if err == nil {
		result.Compilation = completed
	}
	return result, &ExecutionError{CompilationID: result.Compilation.ID, Err: cause}
}

func (s *Service) failWithRollback(
	ctx context.Context,
	result domain.CompilationResult,
	original []byte,
	mode os.FileMode,
	candidateHash string,
	cause error,
) (domain.CompilationResult, error) {
	rollbackCause := cause
	if err := atomicWriteFile(s.options.ConfigPath, original, mode); err != nil {
		rollbackCause = fmt.Errorf("%v; write rollback config failed: %w", cause, err)
		return s.completeRollbackFailure(ctx, result, candidateHash, rollbackCause)
	}
	if err := s.controller.ReloadConfig(ctx, s.options.ConfigPath, ""); err != nil {
		rollbackCause = fmt.Errorf("%v; Mihomo rejected rollback configuration: %w", cause, err)
		return s.completeRollbackFailure(ctx, result, candidateHash, rollbackCause)
	}
	restoredAt := time.Now().UTC()
	rollback, rollbackErr := s.audit.CompleteCompilationRollback(ctx, result.Compilation.ID, domain.RollbackRestored, safeErrorMessage(cause), &restoredAt)
	if rollbackErr == nil {
		result.Rollback = &rollback
	}
	completed, completionErr := s.audit.CompleteCompilation(ctx, result.Compilation.ID, domain.CompilationRolledBack, candidateHash, safeErrorMessage(cause), nil)
	if completionErr == nil {
		result.Compilation = completed
	}
	if rollbackErr != nil || completionErr != nil {
		return result, &ExecutionError{CompilationID: result.Compilation.ID, Err: fmt.Errorf("%v; audit completion failed", cause)}
	}
	return result, &ExecutionError{CompilationID: result.Compilation.ID, Err: cause}
}

func (s *Service) completeRollbackFailure(ctx context.Context, result domain.CompilationResult, candidateHash string, cause error) (domain.CompilationResult, error) {
	failedAt := time.Now().UTC()
	rollback, rollbackErr := s.audit.CompleteCompilationRollback(ctx, result.Compilation.ID, domain.RollbackFailed, safeErrorMessage(cause), &failedAt)
	if rollbackErr == nil {
		result.Rollback = &rollback
	}
	completed, completionErr := s.audit.CompleteCompilation(ctx, result.Compilation.ID, domain.CompilationFailed, candidateHash, safeErrorMessage(cause), nil)
	if completionErr == nil {
		result.Compilation = completed
	}
	if rollbackErr != nil || completionErr != nil {
		return result, &ExecutionError{CompilationID: result.Compilation.ID, Err: fmt.Errorf("%v; audit completion failed", cause)}
	}
	return result, &ExecutionError{CompilationID: result.Compilation.ID, Err: cause}
}

func readRegularFile(path string) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("stat Mihomo config %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("Mihomo config %q must be a regular file", path)
	}
	if info.Size() > maxConfigurationBytes {
		return nil, 0, fmt.Errorf("Mihomo config %q exceeds %d bytes", path, maxConfigurationBytes)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open Mihomo config %q: %w", path, err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxConfigurationBytes+1))
	if err != nil {
		return nil, 0, fmt.Errorf("read Mihomo config %q: %w", path, err)
	}
	if len(contents) > maxConfigurationBytes {
		return nil, 0, fmt.Errorf("Mihomo config %q exceeds %d bytes", path, maxConfigurationBytes)
	}
	return contents, info.Mode().Perm(), nil
}

func atomicWriteFile(path string, contents []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".flowcanvas-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set temporary config mode: %w", err)
	}
	if _, err := temporary.Write(contents); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("sync temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("atomically replace config: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err == nil {
		defer directory.Close()
		if err := directory.Sync(); err != nil {
			return fmt.Errorf("sync config directory: %w", err)
		}
	}
	return nil
}

func contentHash(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func safeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > 2048 {
		return message[:2048]
	}
	return message
}

var _ AuditStore = (*store.Store)(nil)
