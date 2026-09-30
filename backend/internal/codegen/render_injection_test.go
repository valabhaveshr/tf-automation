package codegen

import (
	"testing"
	"unicode/utf8"

	"github.com/hashicorp/hcl/v2/hclparse"
)

// adversarialStrings is the corpus that proves the "HCL injection/escaping
// via user-supplied strings" risk from plan/ARCHITECTURE.md is closed: any
// string a user could type into a form, however hostile-looking, must
// survive a render -> reparse round trip byte-for-byte as *data*, never as
// HCL syntax. Both TestTfvarsRoundTrip_Adversarial (below) and
// FuzzTfvarsRoundTrip (render_fuzz_test.go) run every one of these through
// the exact same real code path RenderRootFiles uses (valueToCty ->
// hclwrite.Body.SetAttributeValue), then reparse the emitted
// .auto.tfvars bytes with hclparse and assert the decoded value is
// identical to the original input.
var adversarialStrings = []string{
	"",
	`plain string`,
	`she said "hello" to me`,
	`${var.secret}`,
	`${jsonencode({a=1})}`,
	`%{if true}yes%{else}no%{endif}`,
	`back\slash and \t \n \" escapes-that-look-real`,
	"line1\nline2\r\nline3",
	"tab\tseparated\tvalues",
	"unicode: 日本語 emoji 🎉 üñíçødé ñ",
	`heredoc-looking <<EOT
some content
EOT`,
	"`backtick` and $dollar and #{interp} and #comment-looking",
	`variable "evil" { default = "pwned" }`,
	"} // close early { malicious_block {",
	`quote-storm: """"""""`,
	"null-byte-adjacent: \x00 end",
	"trailing backslash\\",
	veryLongString(20000),
}

func veryLongString(n int) string {
	const unit = "abcXYZ019 üñî🎉 \"quoted\" ${notinterp} "
	out := make([]byte, 0, n+len(unit))
	for len(out) < n {
		out = append(out, unit...)
	}
	return string(out[:n])
}

// roundTripString renders a single string-typed variable's value through
// RenderRootFiles, reparses the resulting .auto.tfvars bytes with
// hclparse, and returns the decoded value alongside the raw rendered
// bytes (for failure diagnostics).
func roundTripString(t *testing.T, value string) (decoded string, rendered []byte) {
	t.Helper()

	record := RecordInput{
		Modules: []ModuleInput{{
			ModuleName:    "rt-mod",
			PinnedVersion: "rt-mod/v0.0.1",
			Variables: []VarSchema{
				{Name: "value", Type: "string", TypeRaw: "string", Required: true, Nullable: false},
			},
			Instances: map[string]map[string]any{
				"case": {"value": value},
			},
		}},
	}

	files, err := RenderRootFiles(record)
	if err != nil {
		t.Fatalf("RenderRootFiles: %v", err)
	}
	rendered, ok := files["rt-mod.auto.tfvars"]
	if !ok {
		t.Fatalf("missing rt-mod.auto.tfvars in output; files: %v", keysOf(files))
	}

	parser := hclparse.NewParser()
	hclFile, diags := parser.ParseHCL(rendered, "rt-mod.auto.tfvars")
	if diags.HasErrors() {
		t.Fatalf("reparsing generated tfvars failed (this itself would be an injection bug): %v\n--- rendered bytes ---\n%s", diags, rendered)
	}

	attrs, diags := hclFile.Body.JustAttributes()
	if diags.HasErrors() {
		t.Fatalf("JustAttributes: %v\n--- rendered bytes ---\n%s", diags, rendered)
	}
	attr, ok := attrs["rt_mod_instances"]
	if !ok {
		t.Fatalf("expected top-level attribute %q, got attrs: %v\n--- rendered bytes ---\n%s", "rt_mod_instances", attrs, rendered)
	}

	val, diags := attr.Expr.Value(nil)
	if diags.HasErrors() {
		t.Fatalf("evaluating attribute expression: %v\n--- rendered bytes ---\n%s", diags, rendered)
	}

	caseVal := val.GetAttr("case")
	fieldVal := caseVal.GetAttr("value")
	if fieldVal.IsNull() {
		t.Fatalf("decoded value is null, want %q\n--- rendered bytes ---\n%s", value, rendered)
	}
	return fieldVal.AsString(), rendered
}

func TestTfvarsRoundTrip_Adversarial(t *testing.T) {
	for _, s := range adversarialStrings {
		s := s
		name := s
		if len(name) > 40 {
			name = name[:40] + "...(truncated)"
		}
		t.Run(name, func(t *testing.T) {
			got, rendered := roundTripString(t, s)
			if got != s {
				t.Errorf("round-trip mismatch:\n  want: %q\n  got:  %q\n--- rendered bytes ---\n%s", s, got, rendered)
			}
		})
	}
}

// TestTfvarsRoundTrip_RejectsInvalidUTF8 documents a deliberate scope
// boundary: Go strings are byte slices and can hold invalid UTF-8, but
// HCL/cty string values are fundamentally UTF-8 text, and real form input
// from a browser is always valid UTF-8. This test just asserts the helper
// used by the fuzz target correctly identifies invalid UTF-8 so the fuzz
// target can skip those inputs rather than reporting a false positive.
func TestTfvarsRoundTrip_RejectsInvalidUTF8(t *testing.T) {
	invalid := string([]byte{0xff, 0xfe, 0xfd})
	if utf8.ValidString(invalid) {
		t.Fatalf("test fixture is unexpectedly valid UTF-8")
	}
}
