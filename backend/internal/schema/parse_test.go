package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loadFixture reads a testdata/*.tf fixture and parses it, failing the test
// immediately on unexpected fatal errors.
func loadFixture(t *testing.T, name string) []VarSchema {
	t.Helper()
	path := filepath.Join("testdata", name)
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	vars, err := ParseVariablesFile(path, src)
	if err != nil {
		if _, ok := err.(ParseErrors); !ok {
			t.Fatalf("ParseVariablesFile(%s) returned a fatal error: %v", path, err)
		}
		// ParseErrors is non-fatal by design; individual test cases assert
		// on it explicitly where relevant.
	}
	return vars
}

func findVar(t *testing.T, vars []VarSchema, name string) VarSchema {
	t.Helper()
	for _, v := range vars {
		if v.Name == name {
			return v
		}
	}
	t.Fatalf("variable %q not found in parsed result", name)
	return VarSchema{}
}

func TestParseVariablesFile_Simple(t *testing.T) {
	vars := loadFixture(t, "simple.tf")
	if len(vars) != 3 {
		t.Fatalf("expected 3 variables, got %d", len(vars))
	}

	count := findVar(t, vars, "instance_count")
	if count.Type != "number" {
		t.Errorf("instance_count.Type = %q, want %q", count.Type, "number")
	}
	if !count.HasDefault {
		t.Errorf("instance_count.HasDefault = false, want true")
	}
	if count.Default == nil {
		t.Fatalf("instance_count.Default is nil, want 1")
	}
	if got := *count.Default; got != float64(1) {
		t.Errorf("instance_count.Default = %v (%T), want 1", got, got)
	}
	if count.Required {
		t.Errorf("instance_count.Required = true, want false (has a default)")
	}

	name := findVar(t, vars, "name")
	if name.Type != "string" {
		t.Errorf("name.Type = %q, want %q", name.Type, "string")
	}
	if name.Default == nil || *name.Default != "example" {
		t.Errorf("name.Default = %v, want %q", name.Default, "example")
	}

	enabled := findVar(t, vars, "enabled")
	if enabled.Type != "bool" {
		t.Errorf("enabled.Type = %q, want %q", enabled.Type, "bool")
	}
	if enabled.Default == nil || *enabled.Default != true {
		t.Errorf("enabled.Default = %v, want true", enabled.Default)
	}
}

func TestParseVariablesFile_Required(t *testing.T) {
	vars := loadFixture(t, "required.tf")
	image := findVar(t, vars, "image")

	if image.HasDefault {
		t.Errorf("image.HasDefault = true, want false")
	}
	if image.Default != nil {
		t.Errorf("image.Default = %v, want nil (omitted)", image.Default)
	}
	if image.Nullable {
		t.Errorf("image.Nullable = true, want false (explicitly set in fixture)")
	}
	if !image.Required {
		t.Errorf("image.Required = false, want true (has_default=false && nullable=false)")
	}
}

func TestParseVariablesFile_Sensitive(t *testing.T) {
	vars := loadFixture(t, "sensitive.tf")
	pw := findVar(t, vars, "admin_password")

	if !pw.Sensitive {
		t.Errorf("admin_password.Sensitive = false, want true")
	}
	if pw.Type != "string" {
		t.Errorf("admin_password.Type = %q, want %q", pw.Type, "string")
	}
}

func TestParseVariablesFile_Validation(t *testing.T) {
	vars := loadFixture(t, "validation.tf")
	size := findVar(t, vars, "size")

	if len(size.Validations) != 1 {
		t.Fatalf("expected 1 validation block, got %d", len(size.Validations))
	}
	rule := size.Validations[0]
	if !strings.Contains(rule.ConditionRaw, "contains(") {
		t.Errorf("ConditionRaw = %q, want it to contain %q", rule.ConditionRaw, "contains(")
	}
	if rule.ErrorMessage != "size must be one of the approved SKUs." {
		t.Errorf("ErrorMessage = %q, want %q", rule.ErrorMessage, "size must be one of the approved SKUs.")
	}
	if size.Default == nil || *size.Default != "Standard_B2s" {
		t.Errorf("size.Default = %v, want %q", size.Default, "Standard_B2s")
	}
}

func TestParseVariablesFile_Nested(t *testing.T) {
	vars := loadFixture(t, "nested.tf")
	disks := findVar(t, vars, "disks")

	if disks.Type != "list" {
		t.Fatalf("disks.Type = %q, want %q", disks.Type, "list")
	}
	if disks.ElementType == nil {
		t.Fatalf("disks.ElementType is nil, want an object element type")
	}
	elem := disks.ElementType
	if elem.Type != "object" {
		t.Fatalf("disks element Type = %q, want %q", elem.Type, "object")
	}
	if len(elem.ObjectFields) != 3 {
		t.Fatalf("expected 3 object fields, got %d: %#v", len(elem.ObjectFields), elem.ObjectFields)
	}
	nameField, ok := elem.ObjectFields["name"]
	if !ok || nameField.Type != "string" {
		t.Errorf("object_fields[name] = %#v, want type=string present", nameField)
	}
	sizeField, ok := elem.ObjectFields["size_gb"]
	if !ok || sizeField.Type != "number" {
		t.Errorf("object_fields[size_gb] = %#v, want type=number present", sizeField)
	}
	tagsField, ok := elem.ObjectFields["tags"]
	if !ok || tagsField.Type != "map" {
		t.Errorf("object_fields[tags] = %#v, want type=map present", tagsField)
	}
	if tagsField.ElementType == nil || tagsField.ElementType.Type != "string" {
		t.Errorf("object_fields[tags].ElementType = %#v, want type=string", tagsField.ElementType)
	}

	if !strings.HasPrefix(disks.TypeRaw, "list(object(") {
		t.Errorf("disks.TypeRaw = %q, want it to start with %q", disks.TypeRaw, "list(object(")
	}
}

func TestParseVariablesFile_Unsupported(t *testing.T) {
	vars := loadFixture(t, "unsupported.tf")
	weird := findVar(t, vars, "weird")

	if weird.Type != "unsupported" {
		t.Errorf("weird.Type = %q, want %q", weird.Type, "unsupported")
	}
	if weird.TypeRaw != "frobnicate" {
		t.Errorf("weird.TypeRaw = %q, want %q", weird.TypeRaw, "frobnicate")
	}
	if weird.ElementType != nil {
		t.Errorf("weird.ElementType = %#v, want nil", weird.ElementType)
	}
	if weird.ObjectFields != nil {
		t.Errorf("weird.ObjectFields = %#v, want nil", weird.ObjectFields)
	}

	// An unsupported type expression is a deliberate fallback, not a parse
	// error, per ARCHITECTURE.md's "Catalog / schema parsing" edge-case
	// table: "Falls back to a raw-JSON field in the UI rather than failing
	// catalog parse entirely."
	_, err := ParseVariablesFile("unsupported.tf", mustRead(t, "testdata/unsupported.tf"))
	if err != nil {
		t.Errorf("ParseVariablesFile returned error %v for an unsupported-type variable, want nil (non-fatal fallback)", err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return b
}

func TestParseVariablesFile_FatalSyntaxError(t *testing.T) {
	_, err := ParseVariablesFile("bad.tf", []byte(`variable "x" {`))
	if err == nil {
		t.Fatalf("expected a fatal error for malformed HCL, got nil")
	}
	if _, ok := err.(ParseErrors); ok {
		t.Errorf("expected a plain fatal error (unparseable file), got ParseErrors: %v", err)
	}
}
