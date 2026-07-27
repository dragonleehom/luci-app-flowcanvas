package graph

import (
	"errors"
	"fmt"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
)

var (
	ErrNodeNotFound         = errors.New("node not found in current canvas catalog")
	ErrInvalidEdgeDirection = errors.New("only source -> filter -> target edges are allowed")
	ErrForeignFilter        = errors.New("source device may only connect to its own observed filter")
	ErrDuplicateEdge        = errors.New("duplicate canvas edge")
	ErrEmptyNodeID          = errors.New("edge contains an empty node id")
)

type ValidationError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	Err     error          `json:"-"`
}

func (e *ValidationError) Error() string {
	return e.Message
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

func ValidateEdges(nodes []domain.CanvasNode, edges []domain.CanvasEdge) error {
	catalog := make(map[string]domain.CanvasNode, len(nodes))
	for _, node := range nodes {
		catalog[node.ID] = node
	}

	seen := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if edge.Source == "" || edge.Target == "" {
			return &ValidationError{
				Code:    "INVALID_EDGE",
				Message: "连线必须同时包含 source 与 target。",
				Err:     ErrEmptyNodeID,
			}
		}
		key := edge.Source + "\x00" + edge.Target
		if _, exists := seen[key]; exists {
			return &ValidationError{
				Code:    "DUPLICATE_EDGE",
				Message: "不能保存重复连线。",
				Details: map[string]any{"source": edge.Source, "target": edge.Target},
				Err:     ErrDuplicateEdge,
			}
		}
		seen[key] = struct{}{}

		source, ok := catalog[edge.Source]
		if !ok {
			return nodeNotFound(edge.Source)
		}
		target, ok := catalog[edge.Target]
		if !ok {
			return nodeNotFound(edge.Target)
		}

		switch {
		case source.Kind == domain.NodeKindSource && target.Kind == domain.NodeKindFilter:
			if err := validateSourceToFilter(source, target); err != nil {
				return err
			}
			if edge.Kind != "" && edge.Kind != domain.EdgeSourceToFilter {
				return invalidDirection(source, target)
			}
		case source.Kind == domain.NodeKindFilter && target.Kind == domain.NodeKindTarget:
			if edge.Kind != "" && edge.Kind != domain.EdgeFilterToTarget {
				return invalidDirection(source, target)
			}
		default:
			return invalidDirection(source, target)
		}
	}
	return nil
}

func CanonicalizeEdgeKinds(nodes []domain.CanvasNode, edges []domain.CanvasEdge) ([]domain.CanvasEdge, error) {
	if err := ValidateEdges(nodes, edges); err != nil {
		return nil, err
	}
	catalog := make(map[string]domain.CanvasNode, len(nodes))
	for _, node := range nodes {
		catalog[node.ID] = node
	}
	result := make([]domain.CanvasEdge, len(edges))
	for i, edge := range edges {
		if catalog[edge.Source].Kind == domain.NodeKindSource {
			edge.Kind = domain.EdgeSourceToFilter
		} else {
			edge.Kind = domain.EdgeFilterToTarget
		}
		result[i] = edge
	}
	return result, nil
}

func validateSourceToFilter(source, filter domain.CanvasNode) error {
	sourceData, ok := source.Data.(domain.SourceNodeData)
	if !ok {
		return fmt.Errorf("source node %q has invalid data payload", source.ID)
	}
	filterData, ok := filter.Data.(domain.FilterNodeData)
	if !ok {
		return fmt.Errorf("filter node %q has invalid data payload", filter.ID)
	}
	if sourceData.DeviceID != filterData.DeviceID {
		return &ValidationError{
			Code:    "FOREIGN_FILTER",
			Message: "终端只能连接属于自身的动态应用流。",
			Details: map[string]any{
				"sourceDeviceId": sourceData.DeviceID,
				"filterDeviceId": filterData.DeviceID,
			},
			Err: ErrForeignFilter,
		}
	}
	return nil
}

func invalidDirection(source, target domain.CanvasNode) error {
	return &ValidationError{
		Code:    "INVALID_EDGE_DIRECTION",
		Message: "只允许 Source → Filter → Target 的有向连线。",
		Details: map[string]any{
			"sourceKind": source.Kind,
			"targetKind": target.Kind,
		},
		Err: ErrInvalidEdgeDirection,
	}
}

func nodeNotFound(nodeID string) error {
	return &ValidationError{
		Code:    "INVALID_NODE",
		Message: "连线引用的节点不在当前可用画布中。",
		Details: map[string]any{"nodeId": nodeID},
		Err:     ErrNodeNotFound,
	}
}
