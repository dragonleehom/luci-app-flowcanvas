package store

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var initialSchema string

var (
	ErrRevisionConflict = errors.New("canvas revision conflict")
	ErrUnknownNode      = errors.New("node is not part of the current discovery catalog")
)

type RevisionConflictError struct {
	Expected int64
	Actual   int64
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("canvas revision conflict: expected %d, actual %d", e.Expected, e.Actual)
}

func (e *RevisionConflictError) Unwrap() error {
	return ErrRevisionConflict
}

type Store struct {
	db *sql.DB
}

type PersistedCanvas struct {
	Metadata  domain.CanvasMetadata
	Positions map[string]domain.Position
	Edges     []domain.CanvasEdge
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	if path == ":memory:" {
		db.SetMaxOpenConns(1)
	} else {
		db.SetMaxOpenConns(8)
		db.SetMaxIdleConns(4)
	}

	store := &Store{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) configure(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, initialSchema); err != nil {
		return fmt.Errorf("apply initial sqlite schema: %w", err)
	}
	const migrationVersion = 1
	if _, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(?, ?)",
		migrationVersion,
		time.Now().UTC().Unix(),
	); err != nil {
		return fmt.Errorf("record sqlite schema migration: %w", err)
	}
	return s.ensureDefaultCanvas(ctx)
}

func (s *Store) ensureDefaultCanvas(ctx context.Context) error {
	now := time.Now().UTC().Unix()
	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO canvases(id, name, revision, is_default, created_at, updated_at)
		VALUES('default', '默认编排图', 0, 1, ?, ?)`, now, now)
	if err != nil {
		return fmt.Errorf("ensure default canvas: %w", err)
	}
	return nil
}

func (s *Store) LoadDefaultCanvas(ctx context.Context) (PersistedCanvas, error) {
	var result PersistedCanvas
	var updatedAt int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT id, name, revision, updated_at FROM canvases WHERE id = 'default'`,
	).Scan(&result.Metadata.ID, &result.Metadata.Name, &result.Metadata.Revision, &updatedAt); err != nil {
		return PersistedCanvas{}, fmt.Errorf("load default canvas: %w", err)
	}
	result.Metadata.ETag = ETag(result.Metadata.Revision)
	result.Metadata.UpdatedAt = time.Unix(updatedAt, 0).UTC()
	result.Positions = make(map[string]domain.Position)
	result.Edges = make([]domain.CanvasEdge, 0)

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, position_x, position_y FROM canvas_nodes WHERE canvas_id = 'default'`)
	if err != nil {
		return PersistedCanvas{}, fmt.Errorf("load canvas node positions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var position domain.Position
		if err := rows.Scan(&id, &position.X, &position.Y); err != nil {
			return PersistedCanvas{}, fmt.Errorf("scan canvas node position: %w", err)
		}
		result.Positions[id] = position
	}
	if err := rows.Err(); err != nil {
		return PersistedCanvas{}, fmt.Errorf("iterate canvas node positions: %w", err)
	}

	edges, err := s.db.QueryContext(ctx, `
		SELECT id, source_node_id, target_node_id, edge_kind
		FROM canvas_edges WHERE canvas_id = 'default' ORDER BY created_at, id`)
	if err != nil {
		return PersistedCanvas{}, fmt.Errorf("load canvas edges: %w", err)
	}
	defer edges.Close()
	for edges.Next() {
		var edge domain.CanvasEdge
		if err := edges.Scan(&edge.ID, &edge.Source, &edge.Target, &edge.Kind); err != nil {
			return PersistedCanvas{}, fmt.Errorf("scan canvas edge: %w", err)
		}
		result.Edges = append(result.Edges, edge)
	}
	if err := edges.Err(); err != nil {
		return PersistedCanvas{}, fmt.Errorf("iterate canvas edges: %w", err)
	}
	return result, nil
}

func (s *Store) SaveDefaultGraph(
	ctx context.Context,
	expectedRevision int64,
	catalog []domain.CanvasNode,
	request domain.GraphSaveRequest,
) (PersistedCanvas, error) {
	byID := make(map[string]domain.CanvasNode, len(catalog))
	for _, node := range catalog {
		byID[node.ID] = node
	}
	for _, position := range request.NodePositions {
		if _, ok := byID[position.ID]; !ok {
			return PersistedCanvas{}, fmt.Errorf("%w: %s", ErrUnknownNode, position.ID)
		}
	}
	for _, edge := range request.Edges {
		if _, ok := byID[edge.Source]; !ok {
			return PersistedCanvas{}, fmt.Errorf("%w: %s", ErrUnknownNode, edge.Source)
		}
		if _, ok := byID[edge.Target]; !ok {
			return PersistedCanvas{}, fmt.Errorf("%w: %s", ErrUnknownNode, edge.Target)
		}
	}

	positions := make(map[string]domain.Position, len(request.NodePositions))
	for _, node := range request.NodePositions {
		positions[node.ID] = node.Position
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PersistedCanvas{}, fmt.Errorf("begin canvas transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var actualRevision int64
	if err := tx.QueryRowContext(ctx, "SELECT revision FROM canvases WHERE id = 'default'").Scan(&actualRevision); err != nil {
		return PersistedCanvas{}, fmt.Errorf("read canvas revision: %w", err)
	}
	if actualRevision != expectedRevision {
		return PersistedCanvas{}, &RevisionConflictError{Expected: expectedRevision, Actual: actualRevision}
	}

	now := time.Now().UTC().Unix()
	for _, node := range catalog {
		position, ok := positions[node.ID]
		if !ok {
			position = defaultPosition(node.Kind)
		}
		resourceID, err := resourceIDForNode(node)
		if err != nil {
			return PersistedCanvas{}, err
		}
		payload, err := json.Marshal(node.Data)
		if err != nil {
			return PersistedCanvas{}, fmt.Errorf("marshal node %q data: %w", node.ID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO canvas_nodes(id, canvas_id, node_kind, resource_id, position_x, position_y, data_json, created_at, updated_at)
			VALUES(?, 'default', ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			  node_kind=excluded.node_kind,
			  resource_id=excluded.resource_id,
			  position_x=excluded.position_x,
			  position_y=excluded.position_y,
			  data_json=excluded.data_json,
			  updated_at=excluded.updated_at`,
			node.ID, node.Kind, resourceID, position.X, position.Y, string(payload), now, now,
		); err != nil {
			return PersistedCanvas{}, fmt.Errorf("upsert canvas node %q: %w", node.ID, err)
		}
	}

	if _, err := tx.ExecContext(ctx, "DELETE FROM canvas_edges WHERE canvas_id = 'default'"); err != nil {
		return PersistedCanvas{}, fmt.Errorf("replace canvas edges: %w", err)
	}
	for _, edge := range request.Edges {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO canvas_edges(id, canvas_id, source_node_id, target_node_id, edge_kind, created_at)
			VALUES(?, 'default', ?, ?, ?, ?)`,
			edge.ID, edge.Source, edge.Target, edge.Kind, now,
		); err != nil {
			return PersistedCanvas{}, fmt.Errorf("insert canvas edge %q: %w", edge.ID, err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE canvases SET revision = revision + 1, updated_at = ? WHERE id = 'default'", now,
	); err != nil {
		return PersistedCanvas{}, fmt.Errorf("advance canvas revision: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return PersistedCanvas{}, fmt.Errorf("commit canvas graph: %w", err)
	}
	return s.LoadDefaultCanvas(ctx)
}

func ETag(revision int64) string {
	return fmt.Sprintf("canvas-%d", revision)
}

func defaultPosition(kind domain.NodeKind) domain.Position {
	switch kind {
	case domain.NodeKindSource:
		return domain.Position{X: 80, Y: 180}
	case domain.NodeKindFilter:
		return domain.Position{X: 460, Y: 180}
	case domain.NodeKindTarget:
		return domain.Position{X: 840, Y: 180}
	default:
		return domain.Position{}
	}
}

func resourceIDForNode(node domain.CanvasNode) (string, error) {
	switch data := node.Data.(type) {
	case domain.SourceNodeData:
		return data.DeviceID, nil
	case domain.FilterNodeData:
		return data.DeviceApplicationID, nil
	case domain.TargetNodeData:
		return data.ProxyName, nil
	default:
		return "", fmt.Errorf("node %q has unsupported data payload %T", node.ID, node.Data)
	}
}
