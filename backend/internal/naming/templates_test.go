package naming

import (
	"strings"
	"testing"
)

func TestValidateTemplate_AcceptsAllKnownTokens(t *testing.T) {
	tmpl := "prj-{business_short_code}-{env}-{project}-{user_label}-{num_id}"
	if err := ValidateTemplate(tmpl); err != nil {
		t.Fatalf("ValidateTemplate(%q) = %v, want nil", tmpl, err)
	}
}

func TestValidateTemplate_AcceptsTemplateWithNoTokens(t *testing.T) {
	if err := ValidateTemplate("static-name"); err != nil {
		t.Fatalf("ValidateTemplate(static) = %v, want nil", err)
	}
}

func TestValidateTemplate_RejectsUnknownToken(t *testing.T) {
	err := ValidateTemplate("prj-{business_short_code}-{typo_token}")
	if err == nil {
		t.Fatal("ValidateTemplate with unknown token = nil error, want error")
	}
	if !strings.Contains(err.Error(), "typo_token") {
		t.Errorf("error %q does not mention the offending token", err.Error())
	}
}

func TestValidateTemplate_RejectsMultipleUnknownTokens_ReportsBoth(t *testing.T) {
	err := ValidateTemplate("{foo}-{bar}-{env}")
	if err == nil {
		t.Fatal("expected error for two unknown tokens")
	}
	if !strings.Contains(err.Error(), "foo") || !strings.Contains(err.Error(), "bar") {
		t.Errorf("error %q should mention both unknown tokens", err.Error())
	}
}

func TestRenderTemplate_ZeroPaddingAtDifferentWidths(t *testing.T) {
	values := map[string]string{
		"business_short_code": "acme",
		"env":                 "prod",
		"project":             "web",
		"user_label":          "app",
	}

	cases := []struct {
		width int
		numID int
		want  string
	}{
		{width: 2, numID: 1, want: "01"},
		{width: 2, numID: 9, want: "09"},
		{width: 2, numID: 42, want: "42"},
		{width: 2, numID: 99, want: "99"},
		{width: 4, numID: 1, want: "0001"},
		{width: 4, numID: 250, want: "0250"},
		{width: 1, numID: 7, want: "7"},
	}

	for _, c := range cases {
		got, err := RenderTemplate("{num_id}", values, c.numID, c.width)
		if err != nil {
			t.Fatalf("RenderTemplate width=%d numID=%d: unexpected error: %v", c.width, c.numID, err)
		}
		if got != c.want {
			t.Errorf("RenderTemplate width=%d numID=%d = %q, want %q", c.width, c.numID, got, c.want)
		}
	}
}

func TestRenderTemplate_SubstitutesAllTokens(t *testing.T) {
	values := map[string]string{
		"business_short_code": "acme",
		"env":                 "prod",
		"project":             "web",
		"user_label":          "worker",
	}
	got, err := RenderTemplate("prj-{business_short_code}-{env}-{user_label}-{num_id}", values, 3, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "prj-acme-prod-worker-03"
	if got != want {
		t.Errorf("RenderTemplate = %q, want %q", got, want)
	}
}

func TestRenderTemplate_MissingValueErrors(t *testing.T) {
	// project deliberately omitted from values.
	values := map[string]string{
		"business_short_code": "acme",
		"env":                 "prod",
	}
	_, err := RenderTemplate("{business_short_code}-{env}-{project}-{num_id}", values, 1, 2)
	if err == nil {
		t.Fatal("expected error for missing token value, got nil")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error %q should name the missing token", err.Error())
	}
}
