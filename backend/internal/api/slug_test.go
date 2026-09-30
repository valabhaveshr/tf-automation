package api

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"My Project":     "my-project",
		"  Acme, Inc.  ": "acme-inc",
		"already-slug":   "already-slug",
		"":                "x",
		"UPPER_CASE":     "upper-case",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildOutputPath(t *testing.T) {
	got := buildOutputPath("Acme Corp", "Web App", "Prod")
	want := "acme-corp/web-app/prod"
	if got != want {
		t.Errorf("buildOutputPath(...) = %q, want %q", got, want)
	}
}
