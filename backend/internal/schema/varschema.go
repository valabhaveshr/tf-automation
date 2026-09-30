// Package schema implements WS-B (Schema Parser): extracting the frozen
// VarSchema contract (see contracts/var-schema.schema.json) from a Terraform
// module's variables.tf file.
package schema

// ValidationRule mirrors a `validation { condition = ...; error_message = ... }`
// block nested inside a `variable` block. Only the raw condition source text is
// kept (not evaluated) — validations are surfaced as UI help text only; the
// authoritative check happens server-side against the resolved value, per
// ARCHITECTURE.md's Forms/UI edge-case table.
type ValidationRule struct {
	ConditionRaw string `json:"condition_raw"`
	ErrorMessage string `json:"error_message"`
}

// VarSchema is the normalized shape of a single Terraform variable, exactly as
// frozen by contracts/var-schema.schema.json. Field names/tags/shapes here are
// load-bearing: WS-C (codegen) and WS-H (frontend) both build against this
// exact JSON shape.
//
// Field-presence rules (see contracts/var-schema.example.json):
//   - `default` is present in the JSON output if and only if `has_default` is
//     true — including the edge case of an explicit `default = null`, which
//     must still marshal as `"default": null`, not be omitted. This is why
//     Default is a *any rather than a bare any: a non-nil pointer whose
//     pointee is a nil interface still marshals as an explicit JSON null,
//     while a nil pointer is omitted by `omitempty`.
//   - `element_type` / `object_fields` are omitted for scalar types.
//   - `validations` is always emitted (defaulting to `[]`, never `null`).
type VarSchema struct {
	Name         string                `json:"name"`
	Type         string                `json:"type"`
	TypeRaw      string                `json:"type_raw"`
	ElementType  *VarSchema            `json:"element_type,omitempty"`
	ObjectFields map[string]VarSchema  `json:"object_fields,omitempty"`
	Default      *any                  `json:"default,omitempty"`
	HasDefault   bool                  `json:"has_default"`
	Required     bool                  `json:"required"`
	Description  string                `json:"description,omitempty"`
	Sensitive    bool                  `json:"sensitive"`
	Nullable     bool                  `json:"nullable"`
	Validations  []ValidationRule      `json:"validations"`
}

// setDefault records an explicit default value (including a literal nil,
// i.e. `default = null`), producing `"default": null` rather than omitting
// the field. See the VarSchema doc comment for why this needs a pointer.
func (v *VarSchema) setDefault(val any) {
	v.HasDefault = true
	v.Default = &val
}
