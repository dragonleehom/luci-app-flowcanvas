package compiler

import (
	"strings"
	"testing"

	"github.com/dragonleehom/luci-app-flowcanvas/backend/internal/domain"
	"gopkg.in/yaml.v3"
)

func TestMergeManagedOverlayPreservesUserConfiguration(t *testing.T) {
	base := []byte(`# user configuration
mixed-port: 7890
rule-providers:
  user-domains:
    type: file
    behavior: domain
    path: ./user.yaml
  flowcanvas-old:
    type: inline
    behavior: classical
    payload:
      - DOMAIN,old.example
rules:
  - RULE-SET,flowcanvas-old,OldProxy
  - DOMAIN-SUFFIX,example.org,DIRECT
  - MATCH,Proxy
`)
	preview := domain.CompilationPreview{
		Providers: []domain.CompiledProvider{{
			Name: "flowcanvas-new", TargetName: "Proxy-US", Payload: []string{"AND,((SRC-IP-CIDR,192.168.1.50/32),(DOMAIN,v.qq.com))"},
		}},
		Rules: []string{"RULE-SET,flowcanvas-new,Proxy-US"},
	}
	candidate, err := MergeManagedOverlay(base, preview)
	if err != nil {
		t.Fatalf("merge managed overlay: %v", err)
	}
	text := string(candidate)
	for _, preserved := range []string{"mixed-port: 7890", "user-domains:", "DOMAIN-SUFFIX,example.org,DIRECT", "MATCH,Proxy"} {
		if !strings.Contains(text, preserved) {
			t.Fatalf("candidate did not preserve %q:\n%s", preserved, text)
		}
	}
	for _, absent := range []string{"flowcanvas-old", "RULE-SET,flowcanvas-old,OldProxy"} {
		if strings.Contains(text, absent) {
			t.Fatalf("candidate retained obsolete managed entry %q:\n%s", absent, text)
		}
	}
	if !strings.Contains(text, "flowcanvas-new:") || !strings.Contains(text, "RULE-SET,flowcanvas-new,Proxy-US") {
		t.Fatalf("candidate missing new managed entries:\n%s", text)
	}
	if strings.Index(text, "RULE-SET,flowcanvas-new,Proxy-US") > strings.Index(text, "DOMAIN-SUFFIX,example.org,DIRECT") {
		t.Fatalf("managed rules must precede user rules:\n%s", text)
	}
	var parsed yaml.Node
	if err := yaml.Unmarshal(candidate, &parsed); err != nil {
		t.Fatalf("candidate should parse as YAML: %v", err)
	}
}

func TestMergeManagedOverlayRemovesAllManagedEntriesForEmptyPreview(t *testing.T) {
	base := []byte(`rule-providers:
  flowcanvas-old:
    type: inline
    behavior: classical
    payload: [DOMAIN,old.example]
  keep:
    type: inline
    behavior: classical
    payload: [DOMAIN,keep.example]
rules:
  - RULE-SET,flowcanvas-old,OldProxy
  - MATCH,DIRECT
`)
	candidate, err := MergeManagedOverlay(base, domain.CompilationPreview{})
	if err != nil {
		t.Fatalf("clear managed overlay: %v", err)
	}
	text := string(candidate)
	if strings.Contains(text, "flowcanvas-old") || strings.Contains(text, "RULE-SET,flowcanvas-old") {
		t.Fatalf("managed entries were not removed:\n%s", text)
	}
	if !strings.Contains(text, "keep:") || !strings.Contains(text, "MATCH,DIRECT") {
		t.Fatalf("user configuration was removed:\n%s", text)
	}
}

func TestMergeManagedOverlayRejectsWrongRuleContainerType(t *testing.T) {
	_, err := MergeManagedOverlay([]byte("rules: not-a-list\n"), domain.CompilationPreview{})
	if err == nil || !strings.Contains(err.Error(), "rules must be a sequence") {
		t.Fatalf("expected rule type error, got %v", err)
	}
}
