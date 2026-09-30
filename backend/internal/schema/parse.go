package schema

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/ext/typeexpr"
	"github.com/hashicorp/hcl/v2/hclparse"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/convert"
	ctyjson "github.com/zclconf/go-cty/cty/json"
)

// ParseErrors aggregates the non-fatal, per-variable issues collected while
// parsing a variables.tf file. It is intentionally NOT used for whole-file
// syntax errors (those are fatal and returned as a plain error with a nil
// result — see ParseVariablesFile) nor for the "unsupported type" fallback
// (that is a deliberate, non-error outcome per ARCHITECTURE.md's Catalog /
// schema parsing edge-case table: "Falls back to a raw-JSON field ... rather
// than failing catalog parse entirely").
//
// Design choice (documented per the task's "use your judgment" instruction):
// ParseVariablesFile always returns every variable block it found, even ones
// that produced errors — callers get best-effort data plus a named list of
// what went wrong, consistent with the "flagged, not fatal" pattern used
// throughout the edge-case table. A caller that wants strict all-or-nothing
// behavior can simply treat a non-nil error as fatal itself.
type ParseErrors []error

func (pe ParseErrors) Error() string {
	parts := make([]string, len(pe))
	for i, e := range pe {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "; ")
}

var rootBodySchema = &hcl.BodySchema{
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "variable", LabelNames: []string{"name"}},
	},
}

var variableBodySchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "type"},
		{Name: "default"},
		{Name: "description"},
		{Name: "sensitive"},
		{Name: "nullable"},
	},
	Blocks: []hcl.BlockHeaderSchema{
		{Type: "validation"},
	},
}

var validationBodySchema = &hcl.BodySchema{
	Attributes: []hcl.AttributeSchema{
		{Name: "condition", Required: true},
		{Name: "error_message", Required: true},
	},
}

// ParseVariablesFile parses a Terraform variables.tf file's source bytes into
// a normalized []VarSchema (the frozen contract in
// contracts/var-schema.schema.json).
//
// Errors:
//   - An HCL *syntax* error in the file itself (it doesn't parse as HCL at
//     all) is fatal: (nil, error) is returned, matching ARCHITECTURE.md's
//     "variables.tf has an HCL syntax error -> module flagged as invalid,
//     excluded from the catalog" handling — the caller (WS-D's catalog
//     service) is expected to treat that as "exclude this module".
//   - Anything short of that (a single variable block failing to evaluate
//     its default, an unsupported type expression, a validation block
//     missing error_message, etc.) is non-fatal: parsing continues for the
//     remaining blocks, and every problem encountered is collected into a
//     returned ParseErrors naming the offending variable. The []VarSchema
//     result is always populated with everything that *could* be parsed,
//     even for variables that also contributed an error.
func ParseVariablesFile(filename string, src []byte) ([]VarSchema, error) {
	parser := hclparse.NewParser()
	file, diags := parser.ParseHCL(src, filename)
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing %s: %s", filename, diags.Error())
	}

	content, _, diags := file.Body.PartialContent(rootBodySchema)

	var errs []error
	if diags.HasErrors() {
		errs = append(errs, fmt.Errorf("%s: %s", filename, diags.Error()))
	}

	var result []VarSchema
	for _, block := range content.Blocks {
		if block.Type != "variable" || len(block.Labels) != 1 {
			continue
		}
		name := block.Labels[0]
		vs, blockErrs := parseVariableBlock(name, block.Body, src)
		result = append(result, vs)
		for _, e := range blockErrs {
			errs = append(errs, fmt.Errorf("variable %q: %w", name, e))
		}
	}

	if len(errs) > 0 {
		return result, ParseErrors(errs)
	}
	return result, nil
}

func parseVariableBlock(name string, body hcl.Body, src []byte) (VarSchema, []error) {
	var errs []error
	vs := VarSchema{
		Name:        name,
		Validations: []ValidationRule{},
	}

	bodyContent, _, diags := body.PartialContent(variableBodySchema)
	if diags.HasErrors() {
		errs = append(errs, fmt.Errorf("reading block body: %s", diags.Error()))
	}

	// --- type ---
	declaredType := cty.DynamicPseudoType // Terraform's own default when `type` is omitted: "any".
	typeIsUsable := true
	if attr, ok := bodyContent.Attributes["type"]; ok {
		vs.TypeRaw = strings.TrimSpace(string(rangeSource(src, attr.Expr.Range())))
		ty, tyDiags := typeexpr.TypeConstraint(attr.Expr)
		if tyDiags.HasErrors() {
			vs.Type = "unsupported"
			typeIsUsable = false
		} else {
			declaredType = ty
			populateType(&vs, ty)
		}
	} else {
		vs.Type = "any"
		vs.TypeRaw = "any"
	}

	// --- sensitive ---
	if attr, ok := bodyContent.Attributes["sensitive"]; ok {
		val, valDiags := attr.Expr.Value(nil)
		if valDiags.HasErrors() {
			errs = append(errs, fmt.Errorf("evaluating sensitive: %s", valDiags.Error()))
		} else if b, boolErr := ctyToBool(val); boolErr == nil {
			vs.Sensitive = b
		} else {
			errs = append(errs, fmt.Errorf("sensitive: %v", boolErr))
		}
	}

	// --- nullable (Terraform's own default is true when unset) ---
	vs.Nullable = true
	if attr, ok := bodyContent.Attributes["nullable"]; ok {
		val, valDiags := attr.Expr.Value(nil)
		if valDiags.HasErrors() {
			errs = append(errs, fmt.Errorf("evaluating nullable: %s", valDiags.Error()))
		} else if b, boolErr := ctyToBool(val); boolErr == nil {
			vs.Nullable = b
		} else {
			errs = append(errs, fmt.Errorf("nullable: %v", boolErr))
		}
	}

	// --- description ---
	if attr, ok := bodyContent.Attributes["description"]; ok {
		val, valDiags := attr.Expr.Value(nil)
		if valDiags.HasErrors() {
			errs = append(errs, fmt.Errorf("evaluating description: %s", valDiags.Error()))
		} else if s, strErr := ctyToString(val); strErr == nil {
			vs.Description = s
		} else {
			errs = append(errs, fmt.Errorf("description: %v", strErr))
		}
	}

	// --- default ---
	if attr, ok := bodyContent.Attributes["default"]; ok {
		val, valDiags := attr.Expr.Value(nil)
		if valDiags.HasErrors() {
			errs = append(errs, fmt.Errorf("evaluating default: %s", valDiags.Error()))
		} else {
			// Best-effort convert to the declared type so e.g. a `{}` object
			// literal default for a map(string)-typed variable encodes as a
			// JSON object rather than whatever cty inferred it as. If the
			// type is unsupported/unknown, or conversion fails, fall back to
			// the value's own inferred type.
			outVal := val
			if typeIsUsable {
				if converted, convErr := convert.Convert(val, declaredType); convErr == nil {
					outVal = converted
				}
			}
			jsonVal, jsonErr := ctyValueToJSON(outVal)
			if jsonErr != nil {
				errs = append(errs, fmt.Errorf("converting default to JSON: %v", jsonErr))
			} else {
				vs.setDefault(jsonVal)
			}
		}
	}

	// required = has_default==false && nullable==false, per the frozen
	// contract's documented rule (contracts/var-schema.schema.json's
	// "required" description) — NOT a re-derivation of Terraform's own
	// runtime nullability semantics.
	vs.Required = !vs.HasDefault && !vs.Nullable

	// --- validation blocks ---
	for _, block := range bodyContent.Blocks {
		if block.Type != "validation" {
			continue
		}
		rule, ruleErrs := parseValidationBlock(block.Body, src)
		vs.Validations = append(vs.Validations, rule)
		errs = append(errs, ruleErrs...)
	}

	return vs, errs
}

func parseValidationBlock(body hcl.Body, src []byte) (ValidationRule, []error) {
	var errs []error
	rule := ValidationRule{}

	content, _, diags := body.PartialContent(validationBodySchema)
	if diags.HasErrors() {
		errs = append(errs, fmt.Errorf("reading validation block: %s", diags.Error()))
	}

	if attr, ok := content.Attributes["condition"]; ok {
		rule.ConditionRaw = strings.TrimSpace(string(rangeSource(src, attr.Expr.Range())))
	} else {
		errs = append(errs, fmt.Errorf("validation block missing condition"))
	}

	if attr, ok := content.Attributes["error_message"]; ok {
		val, valDiags := attr.Expr.Value(nil)
		if valDiags.HasErrors() {
			errs = append(errs, fmt.Errorf("evaluating validation error_message: %s", valDiags.Error()))
		} else if s, strErr := ctyToString(val); strErr == nil {
			rule.ErrorMessage = s
		} else {
			errs = append(errs, fmt.Errorf("validation error_message: %v", strErr))
		}
	} else {
		errs = append(errs, fmt.Errorf("validation block missing error_message"))
	}

	return rule, errs
}

// populateType maps a resolved cty.Type onto vs.Type plus, recursively,
// vs.ElementType / vs.ObjectFields. vs.TypeRaw is assumed already set by the
// caller (from the original source text for the top-level call).
func populateType(vs *VarSchema, ty cty.Type) {
	switch {
	case ty == cty.String:
		vs.Type = "string"
	case ty == cty.Number:
		vs.Type = "number"
	case ty == cty.Bool:
		vs.Type = "bool"
	case ty == cty.DynamicPseudoType:
		vs.Type = "any"
	case ty.IsListType():
		vs.Type = "list"
		vs.ElementType = nestedVarSchema("", ty.ElementType())
	case ty.IsSetType():
		vs.Type = "set"
		vs.ElementType = nestedVarSchema("", ty.ElementType())
	case ty.IsMapType():
		vs.Type = "map"
		vs.ElementType = nestedVarSchema("value", ty.ElementType())
	case ty.IsObjectType():
		vs.Type = "object"
		fields := make(map[string]VarSchema, len(ty.AttributeTypes()))
		for attrName, attrType := range ty.AttributeTypes() {
			fields[attrName] = *nestedVarSchema(attrName, attrType)
		}
		vs.ObjectFields = fields
	case ty.IsTupleType():
		vs.Type = "tuple"
		elems := ty.TupleElementTypes()
		if len(elems) > 0 {
			// Tuples may be heterogeneous; the schema only has room for one
			// element_type, so the first element's type is used as a
			// representative (form-rendering only needs a shape hint here —
			// tuples are an uncommon Terraform variable type in practice).
			vs.ElementType = nestedVarSchema("", elems[0])
		}
	default:
		vs.Type = "unsupported"
	}
}

// nestedVarSchema builds the VarSchema for a synthesized (non-source-backed)
// nested type position: list/set/tuple elements and object fields. There is
// no original source range for these (the original range only covers the
// whole type expression), so type_raw is reconstructed from the cty.Type
// itself via ctyTypeRawString. The placeholder metadata fields
// (has_default/required/sensitive/nullable/validations) are fixed to the
// same benign values used by contracts/var-schema.example.json's own nested
// "element_type" entry, since these positions don't correspond to an actual
// `variable` declaration.
func nestedVarSchema(name string, ty cty.Type) *VarSchema {
	vs := &VarSchema{
		Name:        name,
		Validations: []ValidationRule{},
	}
	populateType(vs, ty)
	vs.TypeRaw = ctyTypeRawString(ty)
	return vs
}

// ctyTypeRawString reconstructs a typeexpr-style textual type constraint from
// a resolved cty.Type. Used only for nested type positions where no original
// source text is available (see nestedVarSchema); the top-level type_raw
// always prefers the real source text instead.
func ctyTypeRawString(ty cty.Type) string {
	switch {
	case ty == cty.String:
		return "string"
	case ty == cty.Number:
		return "number"
	case ty == cty.Bool:
		return "bool"
	case ty == cty.DynamicPseudoType:
		return "any"
	case ty.IsListType():
		return "list(" + ctyTypeRawString(ty.ElementType()) + ")"
	case ty.IsSetType():
		return "set(" + ctyTypeRawString(ty.ElementType()) + ")"
	case ty.IsMapType():
		return "map(" + ctyTypeRawString(ty.ElementType()) + ")"
	case ty.IsObjectType():
		attrTypes := ty.AttributeTypes()
		names := make([]string, 0, len(attrTypes))
		for n := range attrTypes {
			names = append(names, n)
		}
		sort.Strings(names)
		parts := make([]string, 0, len(names))
		for _, n := range names {
			parts = append(parts, fmt.Sprintf("%s=%s", n, ctyTypeRawString(attrTypes[n])))
		}
		return "object({" + strings.Join(parts, ", ") + "})"
	case ty.IsTupleType():
		elems := ty.TupleElementTypes()
		parts := make([]string, 0, len(elems))
		for _, et := range elems {
			parts = append(parts, ctyTypeRawString(et))
		}
		return "tuple([" + strings.Join(parts, ", ") + "])"
	default:
		return ty.FriendlyName()
	}
}

// ctyToBool converts a static cty.Value to a Go bool, safely rejecting
// null/unknown values instead of panicking (cty.Value.True() panics on a
// null receiver).
func ctyToBool(val cty.Value) (bool, error) {
	converted, err := convert.Convert(val, cty.Bool)
	if err != nil {
		return false, fmt.Errorf("not a bool: %w", err)
	}
	if converted.IsNull() {
		return false, fmt.Errorf("value is null")
	}
	if !converted.IsKnown() {
		return false, fmt.Errorf("value is not known statically")
	}
	return converted.True(), nil
}

// ctyToString converts a static cty.Value to a Go string, safely rejecting
// null/unknown values instead of panicking (cty.Value.AsString() panics on a
// null receiver).
func ctyToString(val cty.Value) (string, error) {
	converted, err := convert.Convert(val, cty.String)
	if err != nil {
		return "", fmt.Errorf("not a string: %w", err)
	}
	if converted.IsNull() {
		return "", fmt.Errorf("value is null")
	}
	if !converted.IsKnown() {
		return "", fmt.Errorf("value is not known statically")
	}
	return converted.AsString(), nil
}

// ctyValueToJSON converts a static cty.Value into a native Go value
// (string/float64/bool/map[string]any/[]any/nil) suitable for assigning to
// VarSchema.Default, by round-tripping through go-cty's JSON encoding (which
// already implements the correct cty -> JSON mapping) and decoding that back
// with encoding/json.
func ctyValueToJSON(val cty.Value) (any, error) {
	raw, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// rangeSource slices the original source bytes covered by an hcl.Range. It
// assumes rng was produced by parsing src itself (true for every Range this
// package uses it on), so the byte offsets are always in bounds.
func rangeSource(src []byte, rng hcl.Range) []byte {
	if rng.Start.Byte < 0 || rng.End.Byte > len(src) || rng.Start.Byte > rng.End.Byte {
		return nil
	}
	return src[rng.Start.Byte:rng.End.Byte]
}
