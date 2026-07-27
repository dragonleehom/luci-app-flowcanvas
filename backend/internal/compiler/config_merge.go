package compiler

import (
	"fmt"
	"strings"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"gopkg.in/yaml.v3"
)

// MergeManagedOverlay replaces only the FlowCanvas-owned provider entries and
// RULE-SET lines inside a complete Mihomo YAML document. All user-owned nodes
// remain in the original yaml.Node tree and are re-emitted with the candidate.
func MergeManagedOverlay(baseYAML []byte, preview domain.CompilationPreview) ([]byte, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(baseYAML, &document); err != nil {
		return nil, fmt.Errorf("parse Mihomo configuration YAML: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("Mihomo configuration root must be a mapping")
	}
	root := document.Content[0]

	providers, err := replaceManagedProviders(mappingValue(root, "rule-providers"), preview.Providers)
	if err != nil {
		return nil, err
	}
	setMappingValue(root, "rule-providers", providers)

	rules, err := replaceManagedRules(mappingValue(root, "rules"), preview.Rules)
	if err != nil {
		return nil, err
	}
	setMappingValue(root, "rules", rules)

	candidate, err := yaml.Marshal(&document)
	if err != nil {
		return nil, fmt.Errorf("encode candidate Mihomo YAML: %w", err)
	}
	if err := ValidateManagedConfiguration(candidate); err != nil {
		return nil, err
	}
	return candidate, nil
}

func ValidateManagedConfiguration(candidate []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(candidate, &document); err != nil {
		return fmt.Errorf("candidate Mihomo YAML is invalid: %w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("candidate Mihomo configuration root must be a mapping")
	}
	root := document.Content[0]
	providers := mappingValue(root, "rule-providers")
	if providers == nil || providers.Kind != yaml.MappingNode {
		return fmt.Errorf("candidate rule-providers must be a mapping")
	}
	rules := mappingValue(root, "rules")
	if rules == nil || rules.Kind != yaml.SequenceNode {
		return fmt.Errorf("candidate rules must be a sequence")
	}
	for index := 0; index < len(providers.Content); index += 2 {
		name := providers.Content[index].Value
		if !strings.HasPrefix(name, ManagedProviderPrefix) {
			continue
		}
		provider := providers.Content[index+1]
		if provider.Kind != yaml.MappingNode {
			return fmt.Errorf("managed rule provider %q must be a mapping", name)
		}
		if scalarMappingValue(provider, "type") != "inline" || scalarMappingValue(provider, "behavior") != "classical" {
			return fmt.Errorf("managed rule provider %q must use inline classical behavior", name)
		}
		payload := mappingValue(provider, "payload")
		if payload == nil || payload.Kind != yaml.SequenceNode {
			return fmt.Errorf("managed rule provider %q must have a payload sequence", name)
		}
		for _, item := range payload.Content {
			if item.Kind != yaml.ScalarNode || !strings.HasPrefix(item.Value, "AND,((SRC-IP-CIDR,") {
				return fmt.Errorf("managed rule provider %q contains an invalid FlowCanvas payload", name)
			}
		}
	}
	for _, item := range rules.Content {
		if item.Kind != yaml.ScalarNode {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(item.Value), "RULE-SET,"+ManagedProviderPrefix) {
			parts := strings.Split(item.Value, ",")
			if len(parts) != 3 || !strings.HasPrefix(parts[1], ManagedProviderPrefix) || strings.TrimSpace(parts[2]) == "" {
				return fmt.Errorf("managed rule %q is malformed", item.Value)
			}
		}
	}
	return nil
}

func replaceManagedProviders(existing *yaml.Node, providers []domain.CompiledProvider) (*yaml.Node, error) {
	if existing != nil && existing.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("existing rule-providers must be a mapping")
	}
	result := &yaml.Node{Kind: yaml.MappingNode}
	if existing != nil {
		for index := 0; index < len(existing.Content); index += 2 {
			key := existing.Content[index]
			value := existing.Content[index+1]
			if strings.HasPrefix(key.Value, ManagedProviderPrefix) {
				continue
			}
			result.Content = append(result.Content, key, value)
		}
	}
	for _, provider := range providers {
		result.Content = append(result.Content, scalarNode(provider.Name), managedProviderNode(provider.Payload))
	}
	return result, nil
}

func replaceManagedRules(existing *yaml.Node, managedRules []string) (*yaml.Node, error) {
	if existing != nil && existing.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("existing rules must be a sequence")
	}
	result := &yaml.Node{Kind: yaml.SequenceNode}
	for _, rule := range managedRules {
		result.Content = append(result.Content, scalarNode(rule))
	}
	if existing == nil {
		return result, nil
	}
	for _, item := range existing.Content {
		if item.Kind == yaml.ScalarNode && strings.HasPrefix(strings.TrimSpace(item.Value), "RULE-SET,"+ManagedProviderPrefix) {
			continue
		}
		result.Content = append(result.Content, item)
	}
	return result, nil
}

func managedProviderNode(payload []string) *yaml.Node {
	return mappingNode(
		scalarNode("type"), scalarNode("inline"),
		scalarNode("behavior"), scalarNode("classical"),
		scalarNode("payload"), sequenceNode(payload),
	)
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1]
		}
	}
	return nil
}

func scalarMappingValue(mapping *yaml.Node, key string) string {
	value := mappingValue(mapping, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func setMappingValue(mapping *yaml.Node, key string, value *yaml.Node) {
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			mapping.Content[index+1] = value
			return
		}
	}
	mapping.Content = append(mapping.Content, scalarNode(key), value)
}
