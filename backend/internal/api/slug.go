package api

import (
	"regexp"
	"strings"
)

var slugInvalidRun = regexp.MustCompile(`[^a-z0-9]+`)

// slugify lowercases s and collapses every run of non-alphanumeric
// characters into a single hyphen, trimming leading/trailing hyphens --
// e.g. "My Project" -> "my-project", matching the edge case
// "Customer/project/env name collides after slugification ... DB UNIQUE
// constraint on the slugified output_path is the authoritative guard."
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugInvalidRun.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		s = "x"
	}
	return s
}

// buildOutputPath computes the "<customer>/<project>/<env>/" path (per
// plan/ARCHITECTURE.md's "output is organized under
// customer-name/project-name/env-name/" locked constraint) that a record's
// generated files are committed under, and that records.output_path
// stores.
func buildOutputPath(customer, project, env string) string {
	return slugify(customer) + "/" + slugify(project) + "/" + slugify(env)
}
