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

	"gopkg.in/yaml.v3"
)

// ModuleMetadata mirrors a module's metadata.yaml, exactly as documented in
// plan/ARCHITECTURE.md § Phase 1 -> Module authoring convention. It is
// required (alongside variables.tf) for a module folder to be
// catalog-eligible; see the "Catalog / schema parsing" edge-case table for
// what happens when it's missing, malformed, or its name doesn't match.
type ModuleMetadata struct {
	Name                 string `yaml:"name" json:"name"`                                     // must match the module's leaf folder name or its full path under modules/
	DisplayName          string `yaml:"display_name" json:"display_name"`
	Category             string `yaml:"category" json:"category"`
	Cloud                string `yaml:"cloud" json:"cloud"`                                     // aws | azure | gcp | multi
	Icon                 string `yaml:"icon" json:"icon"`                                       // key into the frontend's fixed icon set
	Description          string `yaml:"description" json:"description"`
	InstanceLabelField    string `yaml:"instance_label_field" json:"instance_label_field"`       // which variable labels each instance in the UI
	NamingResourceType    string `yaml:"naming_resource_type,omitempty" json:"naming_resource_type,omitempty"`     // optional opt-in, e.g. "gcp_project"
	NamingTargetVariable  string `yaml:"naming_target_variable,omitempty" json:"naming_target_variable,omitempty"` // which variable receives the computed name; defaults to instance_label_field when naming_resource_type is set
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
