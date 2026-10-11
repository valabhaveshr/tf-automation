// Package catalog implements WS-D (Catalog Service + Webhook): orchestrating
// WS-B's variables.tf parser and this package's own metadata.yaml parsing
// across the modules repo, scoped to tagged module versions
// (<module-name>/vX.Y.Z), with a webhook receiver (push + tag-create),
// periodic reconciliation, and an admin-triggered manual refresh -- all
// funneling through the single Service.RefreshAll entry point. See
// plan/ARCHITECTURE.md § Phase 1 -> Module authoring convention and
// § Locked Decisions ("Module versioning", "Catalog refresh") for the full
// spec this package implements.
package catalog

import (
	"fmt"
	"path"
	"strings"

	"gopkg.in/yaml.v3"
)

// ModuleMetadata mirrors a module's metadata.yaml, exactly as documented in
// plan/ARCHITECTURE.md § Phase 1 -> Module authoring convention. It is
// required (alongside variables.tf) for a module folder to be
// catalog-eligible; see the "Catalog / schema parsing" edge-case table for
// what happens when it's missing, malformed, or its name doesn't match.
type ModuleMetadata struct {
	Name                 string `yaml:"name" json:"name"` // must match the module's leaf folder name or its full path under modules/
	DisplayName          string `yaml:"display_name" json:"display_name"`
	Category             string `yaml:"category" json:"category"`
	Cloud                string `yaml:"cloud" json:"cloud"` // aws | azure | gcp | multi
	Icon                 string `yaml:"icon" json:"icon"`   // key into the frontend's fixed icon set
	Description          string `yaml:"description" json:"description"`
	InstanceLabelField   string `yaml:"instance_label_field" json:"instance_label_field"`                         // which variable labels each instance in the UI
	NamingResourceType   string `yaml:"naming_resource_type,omitempty" json:"naming_resource_type,omitempty"`     // optional opt-in, e.g. "gcp_project"
	NamingTargetVariable string `yaml:"naming_target_variable,omitempty" json:"naming_target_variable,omitempty"` // which variable receives the computed name; defaults to instance_label_field when naming_resource_type is set

	// Variables carries per-variable UI hints, in the order the form shows
	// them. Optional: variables without an entry are shown after these,
	// with no example.
	Variables VariableHints `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// VariableHint is one entry of metadata.yaml's `variables:` section. The
// "i" (info) hover text comes from the variable's own description in
// variables.tf; Terraform variable blocks can't carry custom attributes,
// so the example and form layout live here.
type VariableHint struct {
	Name     string `json:"name"`
	Example  string `json:"example,omitempty"`  // shown verbatim on the "e" hover
	Group    string `json:"group,omitempty"`    // form section heading
	Advanced bool   `json:"advanced,omitempty"` // collapsed under "Advanced" by default
}

// VariableHints keeps metadata.yaml's `variables:` mapping in file order (a
// Go map would lose it) and serializes to JSON as an array.
type VariableHints []VariableHint

// UnmarshalYAML reads the `variables:` mapping of variable name -> {example,
// group, advanced}. An example may be written as any YAML value (`20`,
// `false`, a list); non-strings are kept as their YAML text, since the
// example is display-only.
func (h *VariableHints) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("variables: expected a mapping of variable name to hints, got %s", nodeKindName(node.Kind))
	}
	out := make(VariableHints, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, val := node.Content[i], node.Content[i+1]
		hint := VariableHint{Name: key.Value}
		if val.Kind == yaml.MappingNode {
			for j := 0; j+1 < len(val.Content); j += 2 {
				field, fv := val.Content[j].Value, val.Content[j+1]
				switch field {
				case "example":
					hint.Example = yamlNodeText(fv)
				case "group":
					hint.Group = fv.Value
				case "advanced":
					if err := fv.Decode(&hint.Advanced); err != nil {
						return fmt.Errorf("variables.%s.advanced: %w", key.Value, err)
					}
				}
			}
		} else if val.Kind != yaml.ScalarNode || val.Value != "" {
			return fmt.Errorf("variables.%s: expected a mapping with example/group/advanced", key.Value)
		}
		out = append(out, hint)
	}
	*h = out
	return nil
}

func yamlNodeText(n *yaml.Node) string {
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	b, err := yaml.Marshal(n)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.SequenceNode:
		return "a list"
	case yaml.ScalarNode:
		return "a single value"
	default:
		return "something else"
	}
}

// ParseMetadataFile parses a metadata.yaml file's raw bytes and validates
// that its declared `name` matches moduleName -- the tag-derived module
// path it was fetched for. moduleName may be nested ("compute/instance"),
// so `name` may be either the leaf folder name ("instance") or the full
// path ("compute/instance"); module identity is always the path, which is
// unique by construction, so two nested modules sharing a leaf name never
// collide.
//
// Per plan/ARCHITECTURE.md § Edge cases -> "Catalog / schema parsing":
// "metadata.yaml's name field doesn't match its folder name -> Validation
// error at catalog build time, module excluded and flagged." This is
// deliberately a plain error, not a *ModuleIssue -- callers (Service.
// RefreshModule) are the ones responsible for wrapping it into the
// non-fatal, per-module ModuleIssue shape, keeping this function a pure
// parse+validate step with no knowledge of the "flagged, not fatal"
// refresh-orchestration concern.
func ParseMetadataFile(moduleName string, src []byte) (*ModuleMetadata, error) {
	var md ModuleMetadata
	if err := yaml.Unmarshal(src, &md); err != nil {
		return nil, fmt.Errorf("catalog: parsing metadata.yaml: %w", err)
	}
	if md.Name != moduleName && md.Name != path.Base(moduleName) {
		return nil, fmt.Errorf("catalog: metadata.yaml name %q does not match module folder %q (use %q or %q)",
			md.Name, moduleName, path.Base(moduleName), moduleName)
	}
	return &md, nil
}
