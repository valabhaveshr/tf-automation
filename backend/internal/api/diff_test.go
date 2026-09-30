package api

import (
	"strings"
	"testing"
)

func TestUnifiedDiff_NoChange(t *testing.T) {
	if d := unifiedDiff("main.tf", "same\n", "same\n"); d != "" {
		t.Errorf("expected empty diff for identical content, got %q", d)
	}
}

func TestUnifiedDiff_Change(t *testing.T) {
	d := unifiedDiff("main.tf", "line1\nline2\nline3\n", "line1\nlineX\nline3\n")
	if d == "" {
		t.Fatal("expected a non-empty diff")
	}
	for _, want := range []string{"--- a/main.tf", "+++ b/main.tf", "-line2", "+lineX", " line1", " line3"} {
		if !strings.Contains(d, want) {
			t.Errorf("diff missing expected fragment %q; got:\n%s", want, d)
		}
	}
}

func TestUnifiedDiff_NewFile(t *testing.T) {
	d := unifiedDiff("backend.tf", "", "terraform {\n}\n")
	if !strings.Contains(d, "+terraform {") {
		t.Errorf("expected pure-addition diff, got:\n%s", d)
	}
}
