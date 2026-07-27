package compiler

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"unicode"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"gopkg.in/yaml.v3"
)

const ManagedProviderPrefix = "flowcanvas-"

var (
	ErrInvalidCanvasIntent = errors.New("canvas cannot be compiled into an unambiguous routing intent")
	ErrInvalidSourceIP     = errors.New("source node has an invalid IP address")
	ErrInvalidMatch        = errors.New("filter match is invalid")
	ErrInvalidTarget       = errors.New("target proxy name is invalid")
)

type ValidationError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
	Err     error          `json:"-"`
}

func (e *ValidationError) Error() string { return e.Message }
func (e *ValidationError) Unwrap() error { return e.Err }

// Compile converts a saved, strict three-segment canvas into a deterministic
// FlowCanvas-managed overlay. Each target owns one inline classical provider;
// the top-level RULE-SET rule supplies the target adapter.
func Compile(snapshot domain.CanvasSnapshot) (domain.CompilationPreview, error) {
	catalog := make(map[string]domain.CanvasNode, len(snapshot.Nodes))
	for _, node := range snapshot.Nodes {
		catalog[node.ID] = node
	}

	sourcesByFilter := make(map[string][]string)
	targetsByFilter := make(map[string][]string)
	for _, edge := range snapshot.Edges {
		source, sourceExists := catalog[edge.Source]
		target, targetExists := catalog[edge.Target]
		if !sourceExists || !targetExists {
			return domain.CompilationPreview{}, invalidIntent(
				"NODE_UNAVAILABLE",
				"画布规则引用的动态节点当前不可用，无法安全编译。",
				map[string]any{"edgeId": edge.ID, "source": edge.Source, "target": edge.Target},
			)
		}
		switch {
		case source.Kind == domain.NodeKindSource && target.Kind == domain.NodeKindFilter:
			sourcesByFilter[target.ID] = append(sourcesByFilter[target.ID], source.ID)
		case source.Kind == domain.NodeKindFilter && target.Kind == domain.NodeKindTarget:
			targetsByFilter[source.ID] = append(targetsByFilter[source.ID], target.ID)
		default:
			return domain.CompilationPreview{}, invalidIntent(
				"INVALID_EDGE_DIRECTION",
				"仅能编译 Source → Filter → Target 的画布连线。",
				map[string]any{"edgeId": edge.ID},
			)
		}
	}

	filterIDs := make(map[string]struct{}, len(sourcesByFilter)+len(targetsByFilter))
	for id := range sourcesByFilter {
		filterIDs[id] = struct{}{}
	}
	for id := range targetsByFilter {
		filterIDs[id] = struct{}{}
	}
	orderedFilterIDs := make([]string, 0, len(filterIDs))
	for id := range filterIDs {
		orderedFilterIDs = append(orderedFilterIDs, id)
	}
	sort.Strings(orderedFilterIDs)

	rulesByTarget := make(map[string]map[string]struct{})
	warnings := make(map[string]struct{})
	for _, filterID := range orderedFilterIDs {
		sources := uniqueSorted(sourcesByFilter[filterID])
		targets := uniqueSorted(targetsByFilter[filterID])
		if len(sources) != 1 {
			return domain.CompilationPreview{}, invalidIntent(
				"FILTER_SOURCE_AMBIGUOUS",
				"每个已编排的动态应用流必须且只能连接一个终端。",
				map[string]any{"filterId": filterID, "sourceCount": len(sources)},
			)
		}
		if len(targets) != 1 {
			return domain.CompilationPreview{}, invalidIntent(
				"FILTER_TARGET_AMBIGUOUS",
				"每个已编排的动态应用流必须且只能连接一个出口。",
				map[string]any{"filterId": filterID, "targetCount": len(targets)},
			)
		}

		sourceData, filterData, targetData, err := resolveIntentNodes(catalog, sources[0], filterID, targets[0])
		if err != nil {
			return domain.CompilationPreview{}, err
		}
		prefix, err := sourceCIDR(sourceData.IP)
		if err != nil {
			return domain.CompilationPreview{}, err
		}
		matchType, matchValue, err := mihomoMatch(filterData.Match)
		if err != nil {
			return domain.CompilationPreview{}, err
		}
		if err := validateTargetName(targetData.ProxyName); err != nil {
			return domain.CompilationPreview{}, err
		}
		if !targetData.Alive || targetData.State == domain.StateInactive {
			warnings[fmt.Sprintf("出口 %q 当前不可用，但规则仍会保留并在 Mihomo 恢复后生效。", targetData.ProxyName)] = struct{}{}
		}
		rule := fmt.Sprintf("AND,((SRC-IP-CIDR,%s),(%s,%s))", prefix, matchType, matchValue)
		if rulesByTarget[targetData.ProxyName] == nil {
			rulesByTarget[targetData.ProxyName] = make(map[string]struct{})
		}
		rulesByTarget[targetData.ProxyName][rule] = struct{}{}
	}

	targetNames := make([]string, 0, len(rulesByTarget))
	for targetName := range rulesByTarget {
		targetNames = append(targetNames, targetName)
	}
	sort.Strings(targetNames)
	providers := make([]domain.CompiledProvider, 0, len(targetNames))
	topLevelRules := make([]string, 0, len(targetNames))
	for _, targetName := range targetNames {
		payload := mapKeysSorted(rulesByTarget[targetName])
		providerName := domain.StableID("flowcanvas", targetName)
		providers = append(providers, domain.CompiledProvider{
			Name:       providerName,
			TargetName: targetName,
			Payload:    payload,
		})
		topLevelRules = append(topLevelRules, fmt.Sprintf("RULE-SET,%s,%s", providerName, targetName))
	}

	managedYAML, err := renderManagedOverlay(providers, topLevelRules)
	if err != nil {
		return domain.CompilationPreview{}, fmt.Errorf("render managed Mihomo YAML: %w", err)
	}
	hash := sha256.Sum256([]byte(managedYAML))
	return domain.CompilationPreview{
		CanvasID:       snapshot.Canvas.ID,
		CanvasRevision: snapshot.Canvas.Revision,
		Providers:      providers,
		Rules:          topLevelRules,
		ManagedYAML:    managedYAML,
		ContentHash:    hex.EncodeToString(hash[:]),
		Warnings:       mapKeysSorted(warnings),
	}, nil
}

func resolveIntentNodes(
	catalog map[string]domain.CanvasNode,
	sourceID, filterID, targetID string,
) (domain.SourceNodeData, domain.FilterNodeData, domain.TargetNodeData, error) {
	source, sourceExists := catalog[sourceID]
	filter, filterExists := catalog[filterID]
	target, targetExists := catalog[targetID]
	if !sourceExists || !filterExists || !targetExists {
		return domain.SourceNodeData{}, domain.FilterNodeData{}, domain.TargetNodeData{}, invalidIntent(
			"NODE_UNAVAILABLE",
			"编译意图引用的节点当前不可用。",
			map[string]any{"source": sourceID, "filter": filterID, "target": targetID},
		)
	}
	sourceData, sourceOK := source.Data.(domain.SourceNodeData)
	filterData, filterOK := filter.Data.(domain.FilterNodeData)
	targetData, targetOK := target.Data.(domain.TargetNodeData)
	if !sourceOK || !filterOK || !targetOK {
		return domain.SourceNodeData{}, domain.FilterNodeData{}, domain.TargetNodeData{}, invalidIntent(
			"NODE_DATA_INVALID",
			"动态节点数据类型不完整，无法安全编译。",
			map[string]any{"source": sourceID, "filter": filterID, "target": targetID},
		)
	}
	if sourceData.DeviceID != filterData.DeviceID {
		return domain.SourceNodeData{}, domain.FilterNodeData{}, domain.TargetNodeData{}, invalidIntent(
			"FOREIGN_FILTER",
			"终端只能编译属于自身的动态应用流。",
			map[string]any{"sourceDeviceId": sourceData.DeviceID, "filterDeviceId": filterData.DeviceID},
		)
	}
	return sourceData, filterData, targetData, nil
}

func sourceCIDR(rawIP string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil {
		return "", &ValidationError{
			Code:    "INVALID_SOURCE_IP",
			Message: "终端节点没有可编译的合法 IP 地址。",
			Details: map[string]any{"ip": rawIP},
			Err:     ErrInvalidSourceIP,
		}
	}
	bits := 128
	if address.Is4() {
		bits = 32
	}
	return netip.PrefixFrom(address.Unmap(), bits).String(), nil
}

func mihomoMatch(match domain.MatchSpec) (string, string, error) {
	value := strings.TrimSpace(strings.ToLower(match.Value))
	if value == "" || strings.ContainsAny(value, ",\r\n\x00") || containsControl(value) {
		return "", "", &ValidationError{
			Code:    "INVALID_MATCH",
			Message: "动态应用匹配值为空或包含不安全的规则分隔符。",
			Details: map[string]any{"kind": match.Kind, "value": match.Value},
			Err:     ErrInvalidMatch,
		}
	}
	switch match.Kind {
	case domain.MatchDomain:
		if _, err := netip.ParseAddr(value); err == nil {
			return "", "", &ValidationError{Code: "INVALID_MATCH", Message: "DOMAIN 匹配不能使用 IP 字面量。", Details: map[string]any{"value": value}, Err: ErrInvalidMatch}
		}
		return "DOMAIN", value, nil
	case domain.MatchSuffix:
		return "DOMAIN-SUFFIX", strings.TrimPrefix(value, "."), nil
	case domain.MatchKeyword:
		return "DOMAIN-KEYWORD", value, nil
	default:
		return "", "", &ValidationError{
			Code:    "UNSUPPORTED_MATCH_KIND",
			Message: "仅支持精确域名、域名后缀和域名关键词匹配。",
			Details: map[string]any{"kind": match.Kind},
			Err:     ErrInvalidMatch,
		}
	}
}

func validateTargetName(name string) error {
	if strings.TrimSpace(name) == "" || strings.ContainsAny(name, ",\r\n\x00") || containsControl(name) {
		return &ValidationError{
			Code:    "INVALID_TARGET",
			Message: "出口名称为空或包含不安全的规则分隔符。",
			Details: map[string]any{"targetName": name},
			Err:     ErrInvalidTarget,
		}
	}
	return nil
}

func renderManagedOverlay(providers []domain.CompiledProvider, rules []string) (string, error) {
	root := &yaml.Node{Kind: yaml.MappingNode}
	providersNode := &yaml.Node{Kind: yaml.MappingNode}
	for _, provider := range providers {
		providersNode.Content = append(providersNode.Content,
			scalarNode(provider.Name),
			mappingNode(
				scalarNode("type"), scalarNode("inline"),
				scalarNode("behavior"), scalarNode("classical"),
				scalarNode("payload"), sequenceNode(provider.Payload),
			),
		)
	}
	root.Content = append(root.Content,
		scalarNode("rule-providers"), providersNode,
		scalarNode("rules"), sequenceNode(rules),
	)
	encoded, err := yaml.Marshal(root)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func mappingNode(items ...*yaml.Node) *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Content: items}
}

func sequenceNode(values []string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.SequenceNode}
	for _, value := range values {
		node.Content = append(node.Content, scalarNode(value))
	}
	return node
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

func invalidIntent(code, message string, details map[string]any) *ValidationError {
	return &ValidationError{Code: code, Message: message, Details: details, Err: ErrInvalidCanvasIntent}
}

func uniqueSorted(values []string) []string {
	unique := make(map[string]struct{}, len(values))
	for _, value := range values {
		unique[value] = struct{}{}
	}
	return mapKeysSorted(unique)
}

func mapKeysSorted[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
