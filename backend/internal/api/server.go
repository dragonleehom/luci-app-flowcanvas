package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/compiler"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/graph"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/store"
	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/telemetry"
)

type CompilationService interface {
	Validate(ctx context.Context, snapshot domain.CanvasSnapshot) (domain.CompilationResult, error)
	Apply(ctx context.Context, snapshot domain.CanvasSnapshot) (domain.CompilationResult, error)
}

type Server struct {
	store            *store.Store
	catalog          telemetry.Catalog
	events           *EventHub
	startedAt        time.Time
	refreshDiscovery func(context.Context) error
	compilation      CompilationService
}

func NewServer(database *store.Store, catalog telemetry.Catalog) *Server {
	return &Server{
		store:     database,
		catalog:   catalog,
		events:    NewEventHub(),
		startedAt: time.Now().UTC(),
	}
}

func (s *Server) SetDiscoveryRefreshHandler(handler func(context.Context) error) {
	s.refreshDiscovery = handler
}

func (s *Server) SetCompilationService(service CompilationService) {
	s.compilation = service
}

func (s *Server) NotifyDiscoveryChanged(reason string) {
	s.events.Publish("canvas.patch", map[string]any{
		"reason": reason,
		"resync": true,
	})
}

func (s *Server) Router() http.Handler {
	router := chi.NewRouter()
	router.Get("/api/v1/health", s.handleHealth)
	router.Get("/api/v1/canvas", s.handleCanvas)
	router.Get("/api/v1/canvas/events", s.events.ServeHTTP)
	router.Put("/api/v1/canvas/graph", s.handleSaveGraph)
	router.Get("/api/v1/targets", s.handleTargets)
	router.Get("/api/v1/features", s.handleFeatures)
	router.Post("/api/v1/discovery/refresh", s.handleRefreshDiscovery)
	router.Post("/api/v1/compilations/validate", s.handleValidateCompilation)
	router.Post("/api/v1/compilations/apply", s.handleApplyCompilation)
	router.Get("/api/v1/compilations/{id}", s.handleGetCompilation)
	return router
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := "ok"
	var databaseStatus = "ok"
	if err := s.store.Ping(r.Context()); err != nil {
		status = "degraded"
		databaseStatus = "unavailable"
	}
	writeJSON(w, http.StatusOK, response{Data: map[string]any{
		"status":    status,
		"startedAt": s.startedAt,
		"database":  databaseStatus,
		"mihomo":    "pending-phase-2",
	}})
}

func (s *Server) handleCanvas(w http.ResponseWriter, r *http.Request) {
	snapshot, err := s.canvasSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "CANVAS_UNAVAILABLE", "无法读取画布状态。", err, nil)
		return
	}
	w.Header().Set("ETag", strconv.Quote(snapshot.Canvas.ETag))
	writeJSON(w, http.StatusOK, response{Data: snapshot})
}

func (s *Server) handleSaveGraph(w http.ResponseWriter, r *http.Request) {
	expectedRevision, err := parseExpectedRevision(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_IF_MATCH", "保存画布时必须提供有效的 If-Match revision。", err, nil)
		return
	}
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	var request domain.GraphSaveRequest
	if err := decoder.Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "画布请求不是有效 JSON。", err, nil)
		return
	}
	if decoder.More() {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "画布请求只能包含一个 JSON 对象。", nil, nil)
		return
	}

	nodes, discovery, err := s.catalog.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "DISCOVERY_UNAVAILABLE", "当前无法读取动态节点目录。", err, nil)
		return
	}
	canonicalEdges, err := graph.CanonicalizeEdgeKinds(nodes, request.Edges)
	if err != nil {
		s.writeGraphError(w, err)
		return
	}
	request.Edges = canonicalEdges
	persisted, err := s.store.SaveDefaultGraph(r.Context(), expectedRevision, nodes, request)
	if err != nil {
		if conflict := new(store.RevisionConflictError); errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, "CANVAS_REVISION_CONFLICT", "画布已被其他会话更新，请刷新后再保存。", err, map[string]any{"actualRevision": conflict.Actual})
			return
		}
		if errors.Is(err, store.ErrUnknownNode) {
			writeError(w, http.StatusUnprocessableEntity, "INVALID_NODE", "画布包含当前不可用的节点。", err, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "CANVAS_SAVE_FAILED", "无法持久化画布图。", err, nil)
		return
	}
	snapshot := mergeCanvas(persisted, nodes, discovery)
	w.Header().Set("ETag", strconv.Quote(snapshot.Canvas.ETag))
	writeJSON(w, http.StatusOK, response{Data: snapshot})
	s.events.Publish("canvas.patch", map[string]any{"canvasRevision": snapshot.Canvas.Revision, "resync": true})
}

func (s *Server) handleTargets(w http.ResponseWriter, r *http.Request) {
	nodes, _, err := s.catalog.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "DISCOVERY_UNAVAILABLE", "当前无法读取 Mihomo 出口目录。", err, nil)
		return
	}
	targets := make([]domain.TargetNodeData, 0)
	for _, node := range nodes {
		if node.Kind != domain.NodeKindTarget {
			continue
		}
		if data, ok := node.Data.(domain.TargetNodeData); ok {
			targets = append(targets, data)
		}
	}
	writeJSON(w, http.StatusOK, response{Data: targets})
}

func (s *Server) handleFeatures(w http.ResponseWriter, r *http.Request) {
	nodes, _, err := s.catalog.Snapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "DISCOVERY_UNAVAILABLE", "当前无法读取动态应用流。", err, nil)
		return
	}
	features := make([]domain.FilterNodeData, 0)
	for _, node := range nodes {
		if node.Kind != domain.NodeKindFilter {
			continue
		}
		if data, ok := node.Data.(domain.FilterNodeData); ok {
			features = append(features, data)
		}
	}
	writeJSON(w, http.StatusOK, response{Data: features})
}

func (s *Server) handleRefreshDiscovery(w http.ResponseWriter, r *http.Request) {
	if s.refreshDiscovery == nil {
		writeError(w, http.StatusNotImplemented, "DISCOVERY_REFRESH_UNAVAILABLE", "当前运行模式未启用真实发现刷新。", nil, nil)
		return
	}
	ctx, cancel := ContextWithTimeout(r.Context())
	defer cancel()
	if err := s.refreshDiscovery(ctx); err != nil {
		writeError(w, http.StatusBadGateway, "DISCOVERY_REFRESH_FAILED", "刷新 Mihomo 或本地拓扑发现失败。", err, nil)
		return
	}
	s.NotifyDiscoveryChanged("manual-refresh")
	writeJSON(w, http.StatusOK, response{Data: map[string]any{
		"status":      "refreshed",
		"refreshedAt": time.Now().UTC(),
	}})
}

func (s *Server) handleValidateCompilation(w http.ResponseWriter, r *http.Request) {
	if s.compilation == nil {
		writeError(w, http.StatusNotImplemented, "COMPILATION_UNAVAILABLE", "当前运行模式未启用规则编译。", nil, nil)
		return
	}
	snapshot, err := s.canvasSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CANVAS_UNAVAILABLE", "无法读取当前画布，不能编译规则。", err, nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.compilation.Validate(ctx, snapshot)
	if err != nil {
		s.writeCompilationError(w, err, result)
		return
	}
	writeJSON(w, http.StatusOK, response{Data: result})
}

func (s *Server) handleApplyCompilation(w http.ResponseWriter, r *http.Request) {
	if s.compilation == nil {
		writeError(w, http.StatusNotImplemented, "COMPILATION_UNAVAILABLE", "当前运行模式未启用规则编译。", nil, nil)
		return
	}
	expectedRevision, err := parseExpectedRevision(r.Header.Get("If-Match"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_IF_MATCH", "应用规则时必须提供当前画布的 If-Match revision。", err, nil)
		return
	}
	snapshot, err := s.canvasSnapshot(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "CANVAS_UNAVAILABLE", "无法读取当前画布，不能应用规则。", err, nil)
		return
	}
	if expectedRevision != snapshot.Canvas.Revision {
		writeError(w, http.StatusConflict, "CANVAS_REVISION_CONFLICT", "画布已变更，请先重新预览规则。", nil, map[string]any{"actualRevision": snapshot.Canvas.Revision})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	result, err := s.compilation.Apply(ctx, snapshot)
	if err != nil {
		s.writeCompilationError(w, err, result)
		return
	}
	s.NotifyDiscoveryChanged("compilation-applied")
	writeJSON(w, http.StatusOK, response{Data: result})
}

func (s *Server) handleGetCompilation(w http.ResponseWriter, r *http.Request) {
	record, err := s.store.GetCompilation(r.Context(), chi.URLParam(r, "id"))
	if errors.Is(err, store.ErrCompilationNotFound) {
		writeError(w, http.StatusNotFound, "COMPILATION_NOT_FOUND", "找不到指定的编译审计记录。", err, nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "COMPILATION_UNAVAILABLE", "无法读取编译审计记录。", err, nil)
		return
	}
	result := domain.CompilationResult{Compilation: record}
	if rollback, err := s.store.GetCompilationRollback(r.Context(), record.ID); err == nil {
		result.Rollback = &rollback
	}
	writeJSON(w, http.StatusOK, response{Data: result})
}

func (s *Server) writeCompilationError(w http.ResponseWriter, err error, result domain.CompilationResult) {
	details := map[string]any{}
	if result.Compilation.ID != "" {
		details["compilationId"] = result.Compilation.ID
		details["status"] = result.Compilation.Status
	}
	if result.Rollback != nil {
		details["rollbackStatus"] = result.Rollback.Status
	}
	var validation *compiler.ValidationError
	if errors.As(err, &validation) {
		for key, value := range validation.Details {
			details[key] = value
		}
		writeError(w, http.StatusUnprocessableEntity, validation.Code, validation.Message, err, details)
		return
	}
	var execution *compiler.ExecutionError
	if errors.As(err, &execution) && result.Compilation.Status == domain.CompilationRolledBack {
		writeError(w, http.StatusBadGateway, "MIHOMO_RELOAD_ROLLED_BACK", "Mihomo 拒绝候选规则，系统已恢复上一个已知配置。", err, details)
		return
	}
	if errors.As(err, &execution) && result.Rollback != nil && result.Rollback.Status == domain.RollbackFailed {
		writeError(w, http.StatusInternalServerError, "MIHOMO_ROLLBACK_FAILED", "候选规则失败且自动恢复未完成，请立即检查 Mihomo 配置和审计记录。", err, details)
		return
	}
	writeError(w, http.StatusInternalServerError, "COMPILATION_FAILED", "规则编译或应用失败。", err, details)
}

func (s *Server) canvasSnapshot(ctx context.Context) (domain.CanvasSnapshot, error) {
	persisted, err := s.store.LoadDefaultCanvas(ctx)
	if err != nil {
		return domain.CanvasSnapshot{}, err
	}
	nodes, discovery, err := s.catalog.Snapshot(ctx)
	if err != nil {
		return domain.CanvasSnapshot{}, err
	}
	return mergeCanvas(persisted, nodes, discovery), nil
}

func mergeCanvas(persisted store.PersistedCanvas, nodes []domain.CanvasNode, discovery domain.DiscoveryStatus) domain.CanvasSnapshot {
	merged := make([]domain.CanvasNode, len(nodes))
	for index, node := range nodes {
		if position, found := persisted.Positions[node.ID]; found {
			node.Position = position
		}
		merged[index] = node
	}
	return domain.CanvasSnapshot{
		Canvas:    persisted.Metadata,
		Nodes:     merged,
		Edges:     persisted.Edges,
		Discovery: discovery,
	}
}

func (s *Server) writeGraphError(w http.ResponseWriter, err error) {
	var validation *graph.ValidationError
	if errors.As(err, &validation) {
		writeError(w, http.StatusUnprocessableEntity, validation.Code, validation.Message, err, validation.Details)
		return
	}
	writeError(w, http.StatusUnprocessableEntity, "GRAPH_INVALID", "画布图不满足编排约束。", err, nil)
}

type response struct {
	RequestID string `json:"requestId,omitempty"`
	Data      any    `json:"data"`
}

type errorResponse struct {
	RequestID string   `json:"requestId,omitempty"`
	Error     apiError `json:"error"`
}

type apiError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string, err error, details map[string]any) {
	if err != nil {
		if details == nil {
			details = map[string]any{}
		}
		details["cause"] = fmt.Sprintf("%v", err)
	}
	writeJSON(w, status, errorResponse{Error: apiError{Code: code, Message: message, Details: details}})
}

func parseExpectedRevision(header string) (int64, error) {
	value := strings.Trim(strings.TrimSpace(header), "\"")
	if !strings.HasPrefix(value, "canvas-") {
		return 0, fmt.Errorf("unexpected ETag value %q", header)
	}
	revision, err := strconv.ParseInt(strings.TrimPrefix(value, "canvas-"), 10, 64)
	if err != nil || revision < 0 {
		return 0, fmt.Errorf("invalid canvas revision %q", value)
	}
	return revision, nil
}

func ContextWithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 5*time.Second)
}
