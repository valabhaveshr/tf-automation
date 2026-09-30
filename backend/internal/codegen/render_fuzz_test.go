package codegen

import (
	"testing"
	"unicode/utf8"
)

// FuzzTfvarsRoundTrip is the native Go fuzz counterpart to
// TestTfvarsRoundTrip_Adversarial in render_injection_test.go: it feeds
// arbitrary strings through the exact same render -> hclparse re-parse
// round trip and asserts the decoded value is byte-identical to the
// input. This is the automated enforcement of plan/ARCHITECTURE.md's
// "HCL injection/escaping via user-supplied strings" risk mitigation.
//
// NOTE: this package was authored in an environment without Go installed
// (see the WS-C task notes), so this fuzz target has not been run here.
// Before trusting it, run:
//
//	go test ./internal/codegen/... -run FuzzTfvarsRoundTrip -fuzz FuzzTfvarsRoundTrip -fuzztime=60s
//
// which both validates the seed corpus below compiles/passes and lets the
// fuzzer search for a counterexample beyond it.
func FuzzTfvarsRoundTrip(f *testing.F) {
	for _, s := range adversarialStrings {
		f.Add(s)
	}
	// A few extra fuzz-specific seeds targeting HCL/cty-syntax-shaped
	// inputs specifically, on top of the adversarialStrings shared with
	// the table test.
	f.Add("<<-EOT\nindented heredoc\nEOT")
	f.Add(`Aé`)
	f.Add("a\x00b\x00c")
	f.Add(`"""nested-looking"""`)

	f.Fuzz(func(t *testing.T, s string) {
		if !utf8.ValidString(s) {
			// See TestTfvarsRoundTrip_RejectsInvalidUTF8: HCL/cty strings
			// are UTF-8 text; real form input is always valid UTF-8, and
			// Go's fuzz corpus is not constrained to produce it for the
			// `string` type. Non-UTF-8 byte soup is out of scope here.
			t.Skip("input is not valid UTF-8; out of scope for HCL text round-trip")
		}
		got, rendered := roundTripString(t, s)
		if got != s {
			t.Fatalf("round-trip mismatch:\n  want: %q\n  got:  %q\n--- rendered bytes ---\n%s", s, got, rendered)
		}
	})
}
