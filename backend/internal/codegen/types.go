// Package codegen is WS-C: the Codegen Engine. It turns a resolved record
// (module selections + per-instance values) into generated Terraform HCL
// bytes, using github.com/hashicorp/hcl/v2/hclwrite and
// github.com/zclconf/go-cty/cty for every value written into the output.
//
// The single non-negotiable property of this package: no user-supplied or
// module-default *value* is ever written into HCL source text via
// fmt.Sprintf/string concatenation. Every value goes through cty.Value
// construction (cty.StringVal, cty.ObjectVal, cty.ListVal, ...) and is
// emitted via hclwrite's typed encoders (hclwrite.Body.SetAttributeValue /
// hclwrite.TokensForValue). See render.go for the implementation and
// render_injection_test.go for the property test that enforces it.
package codegen

// Validation mirrors a variable's `validation { condition = ...
// error_message = ... }` block, per contracts/var-schema.schema.json. Surfaced
// as UI help text upstream; codegen does not evaluate these.
type Validation struct {
	ConditionRaw string `json:"condition_raw"`
	ErrorMessage string `json:"error_message"`
}

// VarSchema is a minimal, structurally-compatible local declaration of the
// frozen contracts/var-schema.schema.json shape. It is intentionally
// field-for-field aligned (same JSON field names) with that contract so a
// VarSchema decoded from WS-B's real output (or the Phase 0 fixture at
// contracts/var-schema.example.json) can be unmarshaled directly into this
// type without an adapter, and so the two stay structurally compatible even
// before real WS-B/WS-C integration lands.
type VarSchema struct {
	// Name is the variable name exactly as declared in variables.tf.
	Name string `json:"name"`

	// Type is the coarse type kind: string, number, bool, list, set, map,
	// object, tuple, any, or unsupported.
	Type string `json:"type"`

	// TypeRaw is the original HCL type expression text (display-only; not
	// used by the renderer to avoid re-parsing untrusted/arbitrary text).
	TypeRaw string `json:"type_raw"`

	// ElementType is set for list/set/map: the VarSchema-shaped type of
	// elements.
	ElementType *VarSchema `json:"element_type,omitempty"`

	// ObjectFields is set for type=="object": field name -> VarSchema.
	ObjectFields map[string]VarSchema `json:"object_fields,omitempty"`

	// Default is the variable's default value (if any) as a native Go value
	// decoded from JSON (string/float64/bool/map[string]any/[]any/nil).
	// Only meaningful when HasDefault is true.
	Default any `json:"default,omitempty"`

	// HasDefault distinguishes "no default declared" from "default is null".
	HasDefault bool `json:"has_default"`

	// Required is true when HasDefault is false and Nullable is false.
	Required bool `json:"required"`

	// Description is shown as form help text upstream; unused by codegen.
	Description string `json:"description,omitempty"`

	// Sensitive mirrors variables.tf's sensitive = true. Values are still
	// written into generated tfvars per the Phase 1 locked decision; this
	// flag drives a UI warning upstream, not any codegen behavior change.
	Sensitive bool `json:"sensitive"`

	// Nullable mirrors variables.tf's nullable attribute.
	Nullable bool `json:"nullable"`

	// Validations are variable { validation { ... } } blocks, surfaced as UI
	// help text only; not evaluated by codegen.
	Validations []Validation `json:"validations,omitempty"`
}

// ModuleInput is one selected module within a record: which module, which
// pinned git-tag version, its variable schema (as parsed at that pinned
// version), and the set of user-configured instances for it.
type ModuleInput struct {
	// ModuleName is the module's catalog name: its folder path relative to
	// the modules repo's modules directory, e.g. "vm-instance" or, for a
	// nested module, "compute/instance". Validated by RenderRootFiles
	// (slash-separated segments of letters, digits, '-' and '_', starting
	// with a letter). Slashes and hyphens map to underscores for the HCL
	// block label and root variable name ("compute_instance_instances"),
	// slashes to hyphens for the tfvars file name
	// ("compute-instance.auto.tfvars"), and the path is used as-is in the
	// generated module source URL.
	ModuleName string `json:"module_name"`

	// PinnedVersion is the exact git tag pinned at submission time, e.g.
	// "vm-instance/v1.2.0" (per ARCHITECTURE.md's <module-name>/vX.Y.Z
	// tagging convention). Used verbatim as the `?ref=` query value of the
	// generated module source URL.
	PinnedVersion string `json:"pinned_version"`

	// Variables is this module's variable schema (order is significant: it
	// drives the declared field order of the generated root variable's
	// object type and the generated module block's attribute order).
	Variables []VarSchema `json:"variables"`

	// Instances maps instance name -> { variable name -> resolved raw value
	// (as a native Go value, JSON-shaped) }. A value absent from an
	// instance's map falls back to that variable's schema default (if
	// HasDefault) or a null (if Nullable); otherwise it is a render error.
	Instances map[string]map[string]any `json:"instances"`
}

// RecordInput is the full set of module selections for one record (one
// customer/project/env). Modules is deliberately a slice, not a map: caller
// order is preserved and drives the order blocks/files are emitted in.
type RecordInput struct {
	Modules []ModuleInput `json:"modules"`

	// ModulesRepoOrg, ModulesRepoName and ModulesPath build each module's
	// `source = "git::https://github.com/<org>/<repo>.git//<path>/<module>?ref=..."`
	// URL. All optional: org defaults to "org" and repo to "modules"
	// (matching plan/ARCHITECTURE.md's illustrative example literally), and
	// an empty ModulesPath puts the module at the repo root, so callers
	// that haven't wired real config through yet still get a
	// deterministic, valid source URL rather than an error.
	ModulesRepoOrg  string `json:"modules_repo_org,omitempty"`
	ModulesRepoName string `json:"modules_repo_name,omitempty"`
	ModulesPath     string `json:"modules_path,omitempty"`
}
