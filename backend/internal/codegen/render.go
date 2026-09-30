package codegen

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/zclconf/go-cty/cty"
)

// defaultModulesRepoOrg is used when RecordInput.ModulesRepoOrg is empty, so
// that callers which haven't wired real org config through yet still get a
// deterministic, valid module source URL (matching plan/ARCHITECTURE.md's
// illustrative "github.com/org/modules.git" example literally).
const defaultModulesRepoOrg = "org"

// hclIdentRe is a conservative HCL identifier validator: letters, digits,
// underscore, hyphen, not starting with a digit. Applied to module names,
// variable names, and object field names before they are ever emitted as a
// *bare* (unquoted) token — see typeExprTokens/objectTypeTokens/
// moduleObjectTypeTokensOrdered. This is defense-in-depth: these names
// originate from the module's own variables.tf (already required to be
// valid HCL identifiers by the fact hclparse accepted that file upstream in
// WS-B), not from free-text end-user input, but the renderer never trusts
// that invariant blindly for the one code path (type-constraint syntax)
// that cannot go through cty.Value typed encoding.
var hclIdentRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

func isValidHCLIdent(s string) bool {
	return s != "" && hclIdentRe.MatchString(s)
}

// instanceNameRe matches ARCHITECTURE.md's edge-case rule for instance
// names: "lowercase alphanumeric + hyphens, <=63 chars". Authoritative
// enforcement of this belongs to the API layer (WS-G) before values ever
// reach codegen; this is a defensive re-check, not the sole guard — and
// even an instance name that failed this check would still be rendered
// injection-safely, since it is only ever used as a quoted cty object key,
// never as a bare token.
var instanceNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func isValidInstanceName(s string) bool {
	return instanceNameRe.MatchString(s)
}

// ---------------------------------------------------------------------
// Type-constraint token construction.
//
// Terraform's `type = ...` attribute in a `variable` block is not a value
// expression (cty.Value has no representation for a bare `string`/`number`
// type keyword) — it is HCL "type constraint" syntax. hclwrite therefore
// cannot encode it via SetAttributeValue/cty.Value like every other
// attribute in this package. Instead we hand-build the token sequence
// using only hclwrite.Token{Type, Bytes, SpacesBefore} (the raw, stable
// hclwrite primitive) from a strictly limited alphabet of fixed keywords
// (string/number/bool/list/set/map/object/any) and identifiers that have
// already been validated by isValidHCLIdent. No user-supplied *value* ever
// flows through this path — only module-schema field/variable *names* and
// fixed type keywords, both defensively identifier-validated first. This
// is the one part of the renderer that does not go through cty.Value, by
// necessity of what a type constraint is; it is deliberately kept to the
// minimum surface needed and never touches instance data.
// ---------------------------------------------------------------------

func tIdent(name string) *hclwrite.Token {
	return &hclwrite.Token{Type: hclsyntax.TokenIdent, Bytes: []byte(name)}
}

func tPunct(ty hclsyntax.TokenType, s string) *hclwrite.Token {
	return &hclwrite.Token{Type: ty, Bytes: []byte(s)}
}

// wrapCallTokens builds `fn(inner)`, e.g. wrapCallTokens("map", tokensFor("string")) -> `map(string)`.
func wrapCallTokens(fn string, inner hclwrite.Tokens) hclwrite.Tokens {
	out := hclwrite.Tokens{tIdent(fn), tPunct(hclsyntax.TokenOParen, "(")}
	out = append(out, inner...)
	out = append(out, tPunct(hclsyntax.TokenCParen, ")"))
	return out
}

// typeExprTokens renders the type-constraint tokens for a single VarSchema.
// Unrecognized/unsupported/tuple kinds fall back to the `any` type keyword
// (a fixed, constant token — never derived from any dynamic input), which
// is a safe, always-valid Terraform type constraint that accepts any shape;
// this mirrors the Phase 1 "unsupported type -> raw JSON fallback" edge
// case at the codegen layer.
func typeExprTokens(v VarSchema) (hclwrite.Tokens, error) {
	switch v.Type {
	case "string", "number", "bool":
		return hclwrite.Tokens{tIdent(v.Type)}, nil
	case "list", "set", "map":
		if v.ElementType == nil {
			return hclwrite.Tokens{tIdent("any")}, nil
		}
		inner, err := typeExprTokens(*v.ElementType)
		if err != nil {
			return nil, err
		}
		return wrapCallTokens(v.Type, inner), nil
	case "object":
		return objectTypeTokens(v.ObjectFields)
	default: // "any", "tuple", "unsupported", or anything not recognized
		return hclwrite.Tokens{tIdent("any")}, nil
	}
}

// objectTypeTokens builds `object({ field1 = T1, field2 = T2 })` from an
// unordered field map. Field names are sorted for deterministic output
// (Go map iteration order is not stable) and validated as HCL identifiers
// before being emitted as bare tokens.
func objectTypeTokens(fields map[string]VarSchema) (hclwrite.Tokens, error) {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return buildObjectTypeTokens(names, fields)
}

// moduleObjectTypeTokensOrdered builds the same `object({...})` syntax as
// objectTypeTokens, but from an ordered slice (a module's declared
// variable list) so the generated field order matches variables.tf's
// declaration order rather than being resorted.
func moduleObjectTypeTokensOrdered(vars []VarSchema) (hclwrite.Tokens, error) {
	names := make([]string, 0, len(vars))
	fields := make(map[string]VarSchema, len(vars))
	seen := make(map[string]bool, len(vars))
	for _, v := range vars {
		if seen[v.Name] {
			return nil, fmt.Errorf("codegen: duplicate variable name %q", v.Name)
		}
		seen[v.Name] = true
		names = append(names, v.Name)
		fields[v.Name] = v
	}
	return buildObjectTypeTokens(names, fields)
}

func buildObjectTypeTokens(names []string, fields map[string]VarSchema) (hclwrite.Tokens, error) {
	out := hclwrite.Tokens{
		tIdent("object"),
		tPunct(hclsyntax.TokenOParen, "("),
		tPunct(hclsyntax.TokenOBrace, "{"),
	}
	for i, name := range names {
		if !isValidHCLIdent(name) {
			return nil, fmt.Errorf("codegen: field name %q is not a valid HCL identifier", name)
		}
		fieldTokens, err := typeExprTokens(fields[name])
		if err != nil {
			return nil, err
		}
		nameTok := tIdent(name)
		if i > 0 {
			nameTok.SpacesBefore = 1
		}
		out = append(out, nameTok)
		eq := tPunct(hclsyntax.TokenEqual, "=")
		eq.SpacesBefore = 1
		out = append(out, eq)
		if len(fieldTokens) > 0 {
			fieldTokens[0].SpacesBefore = 1
		}
		out = append(out, fieldTokens...)
		if i < len(names)-1 {
			out = append(out, tPunct(hclsyntax.TokenComma, ","))
		}
	}
	out = append(out, tPunct(hclsyntax.TokenCBrace, "}"), tPunct(hclsyntax.TokenCParen, ")"))
	return out, nil
}

// ---------------------------------------------------------------------
// cty.Type derivation (for correctly-typed empty list/set/map values).
// ---------------------------------------------------------------------

func ctyType(v VarSchema) cty.Type {
	switch v.Type {
	case "string":
		return cty.String
	case "number":
		return cty.Number
	case "bool":
		return cty.Bool
	case "list":
		if v.ElementType == nil {
			return cty.List(cty.DynamicPseudoType)
		}
		return cty.List(ctyType(*v.ElementType))
	case "set":
		if v.ElementType == nil {
			return cty.Set(cty.DynamicPseudoType)
		}
		return cty.Set(ctyType(*v.ElementType))
	case "map":
		if v.ElementType == nil {
			return cty.Map(cty.DynamicPseudoType)
		}
		return cty.Map(ctyType(*v.ElementType))
	case "object":
		fields := make(map[string]cty.Type, len(v.ObjectFields))
		for name, f := range v.ObjectFields {
			fields[name] = ctyType(f)
		}
		return cty.Object(fields)
	default:
		return cty.DynamicPseudoType
	}
}

// ---------------------------------------------------------------------
// Value encoding: native Go (JSON-shaped) values -> cty.Value.
//
// This is the injection-safety-critical path: every value that ultimately
// reaches generated HCL source (whether user-supplied or a module default)
// passes through here and comes out as a typed cty.Value, which is then
// written via hclwrite's own typed encoder (Body.SetAttributeValue). At no
// point is a Go string holding a *value* concatenated or Sprintf'd into
// HCL source bytes.
// ---------------------------------------------------------------------

func typeMismatchErr(name, wantType string, raw any) error {
	return fmt.Errorf("codegen: variable %q: expected a %s value, got %T", name, wantType, raw)
}

func toFloat64(raw any) (float64, bool) {
	switch n := raw.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

// valueToCty resolves one variable's value for one instance into a
// cty.Value, honoring "missing -> default -> null (if nullable) -> error"
// fallback, per contracts/var-schema.schema.json's has_default/nullable
// semantics.
func valueToCty(schema VarSchema, raw any, present bool) (cty.Value, error) {
	if !present {
		if schema.HasDefault {
			raw = schema.Default
			present = true
		} else if schema.Nullable {
			return cty.NullVal(ctyType(schema)), nil
		} else {
			return cty.NilVal, fmt.Errorf("codegen: missing required value for variable %q", schema.Name)
		}
	}

	if raw == nil {
		if schema.Nullable {
			return cty.NullVal(ctyType(schema)), nil
		}
		return cty.NilVal, fmt.Errorf("codegen: variable %q is not nullable but value is null", schema.Name)
	}

	switch schema.Type {
	case "string":
		s, ok := raw.(string)
		if !ok {
			return cty.NilVal, typeMismatchErr(schema.Name, "string", raw)
		}
		return cty.StringVal(s), nil

	case "number":
		f, ok := toFloat64(raw)
		if !ok {
			return cty.NilVal, typeMismatchErr(schema.Name, "number", raw)
		}
		return cty.NumberFloatVal(f), nil

	case "bool":
		b, ok := raw.(bool)
		if !ok {
			return cty.NilVal, typeMismatchErr(schema.Name, "bool", raw)
		}
		return cty.BoolVal(b), nil

	case "list", "set":
		arr, ok := raw.([]any)
		if !ok {
			return cty.NilVal, typeMismatchErr(schema.Name, schema.Type, raw)
		}
		elemSchema := VarSchema{Name: schema.Name + "[]", Type: "any", Nullable: true}
		if schema.ElementType != nil {
			elemSchema = *schema.ElementType
		}
		if len(arr) == 0 {
			et := ctyType(elemSchema)
			if schema.Type == "list" {
				return cty.ListValEmpty(et), nil
			}
			return cty.SetValEmpty(et), nil
		}
		elems := make([]cty.Value, len(arr))
		for i, e := range arr {
			ev, err := valueToCty(elemSchema, e, true)
			if err != nil {
				return cty.NilVal, err
			}
			elems[i] = ev
		}
		if schema.Type == "list" {
			return cty.ListVal(elems), nil
		}
		return cty.SetVal(elems), nil

	case "map":
		m, ok := raw.(map[string]any)
		if !ok {
			return cty.NilVal, typeMismatchErr(schema.Name, "map", raw)
		}
		elemSchema := VarSchema{Name: schema.Name + "[]", Type: "any", Nullable: true}
		if schema.ElementType != nil {
			elemSchema = *schema.ElementType
		}
		if len(m) == 0 {
			return cty.MapValEmpty(ctyType(elemSchema)), nil
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		vals := make(map[string]cty.Value, len(m))
		for _, k := range keys {
			ev, err := valueToCty(elemSchema, m[k], true)
			if err != nil {
				return cty.NilVal, err
			}
			vals[k] = ev
		}
		return cty.MapVal(vals), nil

	case "object":
		m, ok := raw.(map[string]any)
		if !ok {
			return cty.NilVal, typeMismatchErr(schema.Name, "object", raw)
		}
		if len(schema.ObjectFields) == 0 {
			return cty.EmptyObjectVal, nil
		}
		vals := make(map[string]cty.Value, len(schema.ObjectFields))
		for name, fieldSchema := range schema.ObjectFields {
			fv, has := m[name]
			v, err := valueToCty(fieldSchema, fv, has)
			if err != nil {
				return cty.NilVal, err
			}
			vals[name] = v
		}
		return cty.ObjectVal(vals), nil

	default: // "any", "tuple", "unsupported": best-effort structural encoding
		return dynamicCty(raw)
	}
}

// dynamicCty encodes an arbitrary JSON-shaped Go value (string/bool/
// float64/json.Number/[]any/map[string]any/nil) into a cty.Value without a
// declared target type, for the "any"/"tuple"/"unsupported" schema
// fallback (mirrors the Phase 1 raw-JSON-field UI fallback). Every branch
// still goes through a cty constructor — never through string formatting.
func dynamicCty(raw any) (cty.Value, error) {
	switch v := raw.(type) {
	case nil:
		return cty.NullVal(cty.DynamicPseudoType), nil
	case string:
		return cty.StringVal(v), nil
	case bool:
		return cty.BoolVal(v), nil
	case float64:
		return cty.NumberFloatVal(v), nil
	case float32:
		return cty.NumberFloatVal(float64(v)), nil
	case int:
		return cty.NumberIntVal(int64(v)), nil
	case int64:
		return cty.NumberIntVal(v), nil
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return cty.NilVal, fmt.Errorf("codegen: invalid number %q: %w", v.String(), err)
		}
		return cty.NumberFloatVal(f), nil
	case []any:
		if len(v) == 0 {
			return cty.EmptyTupleVal, nil
		}
		elems := make([]cty.Value, len(v))
		for i, e := range v {
			ev, err := dynamicCty(e)
			if err != nil {
				return cty.NilVal, err
			}
			elems[i] = ev
		}
		return cty.TupleVal(elems), nil
	case map[string]any:
		if len(v) == 0 {
			return cty.EmptyObjectVal, nil
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		vals := make(map[string]cty.Value, len(v))
		for _, k := range keys {
			ev, err := dynamicCty(v[k])
			if err != nil {
				return cty.NilVal, err
			}
			vals[k] = ev
		}
		return cty.ObjectVal(vals), nil
	default:
		return cty.NilVal, fmt.Errorf("codegen: unsupported dynamic value type %T", raw)
	}
}

// ---------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------

// RenderRootFiles renders the full set of root Terraform files for one
// record: "variables.tf", "main.tf", and one "<module_name>.auto.tfvars"
// per selected module. The returned map's byte values are exactly what
// hclwrite produced — no post-processing string manipulation is applied to
// them.
func RenderRootFiles(record RecordInput) (map[string][]byte, error) {
	if len(record.Modules) == 0 {
		return nil, fmt.Errorf("codegen: record has zero module selections")
	}

	org := record.ModulesRepoOrg
	if org == "" {
		org = defaultModulesRepoOrg
	}

	varsFile := hclwrite.NewEmptyFile()
	mainFile := hclwrite.NewEmptyFile()
	out := make(map[string][]byte, len(record.Modules)*2+2)

	seenModules := make(map[string]bool, len(record.Modules))

	for i, mod := range record.Modules {
		if !isValidHCLIdent(mod.ModuleName) {
			return nil, fmt.Errorf("codegen: module name %q is not a valid identifier", mod.ModuleName)
		}
		if seenModules[mod.ModuleName] {
			return nil, fmt.Errorf("codegen: duplicate module %q in record", mod.ModuleName)
		}
		seenModules[mod.ModuleName] = true

		sanitized := strings.ReplaceAll(mod.ModuleName, "-", "_")
		if !isValidHCLIdent(sanitized) {
			return nil, fmt.Errorf("codegen: module name %q does not produce a valid HCL identifier", mod.ModuleName)
		}
		instancesVarName := sanitized + "_instances"

		varNames := make(map[string]bool, len(mod.Variables))
		for _, v := range mod.Variables {
			if !isValidHCLIdent(v.Name) {
				return nil, fmt.Errorf("codegen: module %q: variable name %q is not a valid HCL identifier", mod.ModuleName, v.Name)
			}
			if varNames[v.Name] {
				return nil, fmt.Errorf("codegen: module %q: duplicate variable name %q", mod.ModuleName, v.Name)
			}
			varNames[v.Name] = true
		}

		// ---- variables.tf: variable "<mod>_instances" { type = map(object({...})) } ----
		if i > 0 {
			varsFile.Body().AppendNewline()
		}
		varBlock := varsFile.Body().AppendNewBlock("variable", []string{instancesVarName})
		objTokens, err := moduleObjectTypeTokensOrdered(mod.Variables)
		if err != nil {
			return nil, fmt.Errorf("codegen: module %q: %w", mod.ModuleName, err)
		}
		varBlock.Body().SetAttributeRaw("type", wrapCallTokens("map", objTokens))

		// ---- main.tf: module "<mod>" { source, for_each, <var> = each.value.<var> ... } ----
		if i > 0 {
			mainFile.Body().AppendNewline()
		}
		modBlock := mainFile.Body().AppendNewBlock("module", []string{sanitized})
		modBody := modBlock.Body()

		sourceURL := fmt.Sprintf(
			"git::https://github.com/%s/modules.git//%s?ref=%s",
			org, mod.ModuleName, mod.PinnedVersion,
		)
		// sourceURL is Go-side string assembly, but it never reaches HCL
		// source text directly: it is wrapped as a single cty.StringVal and
		// handed to hclwrite's typed encoder below, which quote-escapes it
		// like any other string value.
		modBody.SetAttributeValue("source", cty.StringVal(sourceURL))

		modBody.SetAttributeTraversal("for_each", hcl.Traversal{
			hcl.TraverseRoot{Name: "var"},
			hcl.TraverseAttr{Name: instancesVarName},
		})
		for _, v := range mod.Variables {
			modBody.SetAttributeTraversal(v.Name, hcl.Traversal{
				hcl.TraverseRoot{Name: "each"},
				hcl.TraverseAttr{Name: "value"},
				hcl.TraverseAttr{Name: v.Name},
			})
		}

		// ---- <module>.auto.tfvars: <mod>_instances = { "<instance>" = {...} } ----
		instanceNames := make([]string, 0, len(mod.Instances))
		for name := range mod.Instances {
			instanceNames = append(instanceNames, name)
		}
		sort.Strings(instanceNames)

		instanceVals := make(map[string]cty.Value, len(instanceNames))
		for _, instName := range instanceNames {
			if !isValidInstanceName(instName) {
				return nil, fmt.Errorf("codegen: module %q: instance name %q is not valid (expected lowercase alphanumeric + hyphens, <=63 chars)", mod.ModuleName, instName)
			}
			fields := mod.Instances[instName]
			fieldVals := make(map[string]cty.Value, len(mod.Variables))
			for _, v := range mod.Variables {
				raw, present := fields[v.Name]
				val, err := valueToCty(v, raw, present)
				if err != nil {
					return nil, fmt.Errorf("codegen: module %q instance %q: %w", mod.ModuleName, instName, err)
				}
				fieldVals[v.Name] = val
			}
			if len(fieldVals) == 0 {
				instanceVals[instName] = cty.EmptyObjectVal
			} else {
				instanceVals[instName] = cty.ObjectVal(fieldVals)
			}
		}

		var topVal cty.Value
		if len(instanceVals) == 0 {
			topVal = cty.EmptyObjectVal
		} else {
			topVal = cty.ObjectVal(instanceVals)
		}

		tfvarsFile := hclwrite.NewEmptyFile()
		tfvarsFile.Body().SetAttributeValue(instancesVarName, topVal)
		out[mod.ModuleName+".auto.tfvars"] = formatted(tfvarsFile)
	}

	out["variables.tf"] = formatted(varsFile)
	out["main.tf"] = formatted(mainFile)
	return out, nil
}

// formatted runs an hclwrite.File's bytes through hclwrite.Format, the same
// canonical formatter `terraform fmt` uses (aligned attribute `=` columns,
// standard 2-space indentation). This is applied uniformly to every
// generated file so output matches plan/ARCHITECTURE.md's illustrative
// examples and stays clean against the Phase 1 exit criterion's
// `terraform fmt`-equivalent expectations. It operates purely on already-
// safely-encoded token bytes (whitespace/alignment only) and never touches
// string/value content.
func formatted(f *hclwrite.File) []byte {
	return hclwrite.Format(f.Bytes())
}

// RenderBackendFile renders the one-time root backend.tf:
//
//	terraform {
//	  backend "gcs" {   # or "s3", per STATE_BACKEND config
//	    bucket = "<bucket>"
//	    prefix = "<prefix>"
//	  }
//	}
//
// Callers (WS-G) invoke this exactly once, at a record's first save; it is
// never regenerated on routine edits. bucket and prefix are written via
// cty.StringVal/SetAttributeValue like every other value in this package.
func RenderBackendFile(backendType string, bucket, prefix string) []byte {
	f := hclwrite.NewEmptyFile()
	tfBlock := f.Body().AppendNewBlock("terraform", nil)
	backendBlock := tfBlock.Body().AppendNewBlock("backend", []string{backendType})
	backendBody := backendBlock.Body()
	backendBody.SetAttributeValue("bucket", cty.StringVal(bucket))
	backendBody.SetAttributeValue("prefix", cty.StringVal(prefix))
	return formatted(f)
}
