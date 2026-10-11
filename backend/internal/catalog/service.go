package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/schema"
)

// ModuleIssue is the typed, non-fatal error returned by RefreshModule (and
// collected by RefreshAll) whenever a single module fails to catalog --
// missing/malformed files, a metadata.yaml name mismatch, no released tag
// yet, a persistence failure, etc. Per plan/ARCHITECTURE.md's "Catalog /
// schema parsing" edge-case table, every one of these outcomes is "flagged,
// not fatal": the rest of the catalog refresh must proceed undisturbed.
type ModuleIssue struct {
	ModuleName string
	Reason     string
}

func (i *ModuleIssue) Error() string {
	return fmt.Sprintf("catalog: module %q: %s", i.ModuleName, i.Reason)
}

func newModuleIssue(moduleName, format string, args ...any) *ModuleIssue {
	return &ModuleIssue{ModuleName: moduleName, Reason: fmt.Sprintf(format, args...)}
}

// asModuleIssue reports whether err is a *ModuleIssue, assigning it to
// target if so. Kept as a small local helper (rather than errors.As) since
// *ModuleIssue never wraps another error via Unwrap.
func asModuleIssue(err error, target **ModuleIssue) bool {
	mi, ok := err.(*ModuleIssue)
	if ok {
		*target = mi
	}
	return ok
}

// ModuleSchemaStore is the subset of WS-A's *data.Store this package needs.
// Declared as a small interface -- rather than depending on *data.Store
// directly -- purely so unit tests can substitute an in-memory fake instead
// of a real Postgres connection; production code always passes a real
// *data.Store (see NewService), which satisfies this interface as-is.
type ModuleSchemaStore interface {
	UpsertModuleSchema(ctx context.Context, ms *data.ModuleSchema) (*data.ModuleSchema, error)
}

// Service is WS-D's core: orchestrates listing tags from the modules repo,
// parsing variables.tf (via WS-B) + metadata.yaml (via this package) at the
// highest-semver tag per module, and caching the result via WS-A's
// module_schemas store. Both the webhook handler (webhook.go), the periodic
// reconciliation job (reconcile.go), and WS-G's admin-triggered manual
// refresh endpoint drive the catalog exclusively through this type's
// exported methods.
type Service struct {
	Source      ModulesRepoSource
	Store       ModuleSchemaStore
	ModulesPath string // repo-relative directory containing module folders, e.g. "modules"
}

// NewService builds a Service backed by a real *data.Store. modulesPath is
// the repo-relative directory containing module folders (see
// plan/ARCHITECTURE.md's "modules/<name>/..." convention); it defaults to
// "modules" when empty.
func NewService(source ModulesRepoSource, store *data.Store, modulesPath string) *Service {
	if modulesPath == "" {
		modulesPath = "modules"
	}
	return &Service{Source: source, Store: store, ModulesPath: modulesPath}
}

// moduleTagPattern matches the "<module-name>/vX.Y.Z" tagging convention
// (plan/ARCHITECTURE.md § Locked Decisions: "Module versioning"). The
// module name is everything before the final "/vX.Y.Z" segment; the version
// must be a plain MAJOR.MINOR.PATCH (no pre-release/build metadata), which
// keeps semver comparison in highestTagForModule unambiguous.
var moduleTagPattern = regexp.MustCompile(`^(.+)/v(\d+)\.(\d+)\.(\d+)$`)

// modulePathPattern restricts module names to slash-separated folder
// segments under ModulesPath, e.g. "vm-instance" or "compute/instance".
// Modules may be nested to any depth; the module name IS the folder path
// relative to ModulesPath, which keeps names unique across the repo (two
// leaf folders both named "instance" under different parents are distinct
// modules). The first character must be a letter and segments can't be
// "." / ".." -- the name later becomes an HCL identifier (slashes and
// hyphens mapped to underscores, see codegen.RenderRootFiles, which
// enforces the same rule) and a repo-relative path.
var modulePathPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(/[A-Za-z0-9][A-Za-z0-9_-]*)*$`)

// parseTagName splits a git tag into its module name and version, per the
// "<module-name>/vX.Y.Z" convention (monorepo, path-scoped tags -- a tag
// applies to exactly one module). ok is false for any tag that doesn't
// match the convention (a non-module tag, missing version, or a version
// with pre-release/build metadata) -- such tags are simply not
// catalog-relevant, not an error condition.
func parseTagName(tag string) (moduleName, version string, ok bool) {
	m := moduleTagPattern.FindStringSubmatch(tag)
	if m == nil {
		return "", "", false
	}
	return m[1], "v" + m[2] + "." + m[3] + "." + m[4], true
}

// moduleNameFromTag is parseTagName plus one allowance: the tag may repeat
// the modules directory as a prefix, so for ModulesPath "modules" both
// "compute/instance/v1.0.0" and "modules/compute/instance/v1.0.0" name the
// module "compute/instance" (the folder path relative to ModulesPath). A tag
// is always read as prefixed when it starts with "<ModulesPath>/", so a
// folder literally named like ModulesPath directly under it
// (modules/modules/x) can't be tagged unprefixed.
func moduleNameFromTag(tag, modulesPath string) (moduleName, version string, ok bool) {
	moduleName, version, ok = parseTagName(tag)
	if !ok {
		return "", "", false
	}
	if prefix := strings.Trim(modulesPath, "/"); prefix != "" {
		moduleName = strings.TrimPrefix(moduleName, prefix+"/")
	}
	return moduleName, version, true
}

// semver is a minimal MAJOR.MINOR.PATCH comparator -- deliberately not a
// full semver implementation (no pre-release/build metadata support) since
// moduleTagPattern already restricts tags to that shape.
type semver struct {
	major, minor, patch int
}

func parseSemver(v string) (semver, bool) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return semver{}, false
		}
		nums[i] = n
	}
	return semver{major: nums[0], minor: nums[1], patch: nums[2]}, true
}

func (s semver) less(other semver) bool {
	if s.major != other.major {
		return s.major < other.major
	}
	if s.minor != other.minor {
		return s.minor < other.minor
	}
	return s.patch < other.patch
}

// highestTagForModule returns the full tag string ("<module-name>/vX.Y.Z")
// with the highest semver among tags belonging to moduleName. Per
// plan/ARCHITECTURE.md's "Module has multiple tags" edge case: "Phase 1
// pins to the latest tag (by semver, not by tag-creation date)". ok is
// false if no tag matches moduleName -- the "no released version" case.
//
// Tags with and without the modules-directory prefix (see
// moduleNameFromTag) count as the same module, so the highest version wins
// regardless of which form each tag used.
func highestTagForModule(tags []string, moduleName, modulesPath string) (tag, version string, ok bool) {
	var bestTag, bestVersion string
	var best semver
	found := false
	for _, t := range tags {
		name, ver, matched := moduleNameFromTag(t, modulesPath)
		if !matched || name != moduleName {
			continue
		}
		sv, svOK := parseSemver(ver)
		if !svOK {
			continue
		}
		if !found || best.less(sv) {
			best = sv
			bestTag = t
			bestVersion = ver
			found = true
		}
	}
	return bestTag, bestVersion, found
}

// RefreshModule refreshes the cataloged schema for a single module: finds
// its highest-semver "<moduleName>/vX.Y.Z" tag, fetches variables.tf +
// metadata.yaml at that tag, parses both, and upserts a module_schemas row
// (is_latest=true).
//
// On any failure -- no released tag yet, a missing/malformed variables.tf
// or metadata.yaml, a metadata.yaml name mismatch, or a persistence error
// -- RefreshModule returns a *ModuleIssue naming the module and reason. It
// never panics and never touches any other module's cached state, so
// callers (RefreshAll, the webhook handler, WS-G's admin manual-refresh
// endpoint) can flag this one module without aborting anything else, per
// the "flagged, not fatal" pattern used throughout
// plan/ARCHITECTURE.md's Catalog / schema parsing edge-case table.
//
// Note: this calls Source.ListTags itself (rather than accepting
// pre-fetched tags), matching the exact signature this package's spec
// calls for. Called directly (e.g. from the webhook's targeted single-tag
// refresh) this is one extra list-tags round trip beyond what's strictly
// needed; called from RefreshAll it means tags are listed twice (once to
// group modules, once per module here) -- a deliberate simplicity-over-
// efficiency trade-off, since ListTags is cheap and the whole point of the
// targeted webhook path is "cheap, single small refresh".
func (s *Service) RefreshModule(ctx context.Context, moduleName string) error {
	if !modulePathPattern.MatchString(moduleName) {
		return newModuleIssue(moduleName, "invalid module path: each folder segment must be letters, digits, '-' or '_', and the path must start with a letter")
	}

	tags, err := s.Source.ListTags(ctx)
	if err != nil {
		return fmt.Errorf("catalog: refreshing %q: listing tags: %w", moduleName, err)
	}

	tag, version, ok := highestTagForModule(tags, moduleName, s.ModulesPath)
	if !ok {
		return newModuleIssue(moduleName, "no released version: no %s/vX.Y.Z tag found", moduleName)
	}

	// moduleName is the folder path relative to ModulesPath, so a nested
	// module "compute/instance" is read from modules/compute/instance/.
	varsPath := s.ModulesPath + "/" + moduleName + "/variables.tf"
	metaPath := s.ModulesPath + "/" + moduleName + "/metadata.yaml"

	varsSrc, err := s.Source.GetFileAtRef(ctx, varsPath, tag)
	if err != nil {
		return newModuleIssue(moduleName, "fetching variables.tf at %s: %v", tag, err)
	}
	metaSrc, err := s.Source.GetFileAtRef(ctx, metaPath, tag)
	if err != nil {
		return newModuleIssue(moduleName, "fetching metadata.yaml at %s: %v", tag, err)
	}

	varSchemas, parseErr := schema.ParseVariablesFile(varsPath, varsSrc)
	if parseErr != nil {
		if _, isSoft := parseErr.(schema.ParseErrors); !isSoft {
			// A fatal HCL syntax error (schema.ParseVariablesFile returns
			// (nil, error) for these, per its own doc comment): exclude the
			// module entirely, per "variables.tf has an HCL syntax error ->
			// module flagged as invalid ... excluded from the catalog".
			return newModuleIssue(moduleName, "variables.tf has an HCL syntax error: %v", parseErr)
		}
		// schema.ParseErrors is WS-B's own non-fatal, per-variable error
		// list -- varSchemas is still populated with everything that could
		// be parsed. Phase 1 doesn't have a separate place to surface
		// per-variable parse warnings distinct from the module-level flag,
		// so the module still catalogs successfully here with whatever
		// schema was recovered (individual unsupported-type variables
		// already degrade gracefully to a raw-JSON field per WS-B/the UI
		// edge-case table, rather than needing this module excluded).
	}

	md, err := ParseMetadataFile(moduleName, metaSrc)
	if err != nil {
		return newModuleIssue(moduleName, "metadata.yaml invalid: %v", err)
	}

	metadataJSON, err := json.Marshal(md)
	if err != nil {
		return newModuleIssue(moduleName, "encoding metadata: %v", err)
	}
	variablesJSON, err := json.Marshal(varSchemas)
	if err != nil {
		return newModuleIssue(moduleName, "encoding variables: %v", err)
	}

	_, err = s.Store.UpsertModuleSchema(ctx, &data.ModuleSchema{
		ModuleName: moduleName,
		VersionTag: tag,
		Metadata:   metadataJSON,
		Variables:  variablesJSON,
		IsLatest:   true,
	})
	if err != nil {
		return newModuleIssue(moduleName, "persisting schema for %s (%s): %v", moduleName, version, err)
	}

	return nil
}

// RefreshAll lists every tag in the modules repo, groups them by module name
// (per the tagging convention -- untagged module folders are structurally
// excluded here since they contribute no tag to group by, matching "Module
// has commits but no <module-name>/vX.Y.Z tag yet -> Excluded from the
// catalog"), and refreshes each distinct module in turn via RefreshModule.
//
// A failure refreshing one module is collected into issues and does NOT
// abort the rest -- the "flagged, not fatal" pattern from
// plan/ARCHITECTURE.md's edge-case table. The returned err is only non-nil
// for a whole-refresh failure (e.g. ListTags itself failing), never for an
// individual module's issue.
//
// This is the single function both the periodic reconciliation job
// (reconcile.go's RunPeriodically) and WS-G's admin-triggered manual
// refresh endpoint call, per plan/ARCHITECTURE.md's Catalog refresh locked
// decision: both fallback triggers funnel through the same refresh logic
// the webhook's targeted path (RefreshModule) uses.
func (s *Service) RefreshAll(ctx context.Context) ([]*ModuleIssue, error) {
	tags, err := s.Source.ListTags(ctx)
	if err != nil {
		return nil, fmt.Errorf("catalog: refresh all: listing tags: %w", err)
	}

	moduleNames := make(map[string]struct{})
	for _, t := range tags {
		name, _, ok := moduleNameFromTag(t, s.ModulesPath)
		if !ok {
			continue
		}
		moduleNames[name] = struct{}{}
	}

	names := make([]string, 0, len(moduleNames))
	for n := range moduleNames {
		names = append(names, n)
	}
	sort.Strings(names) // deterministic order for logs/tests

	var issues []*ModuleIssue
	for _, name := range names {
		if refreshErr := s.RefreshModule(ctx, name); refreshErr != nil {
			var issue *ModuleIssue
			if asModuleIssue(refreshErr, &issue) {
				issues = append(issues, issue)
				continue
			}
			// RefreshModule is documented to only ever return *ModuleIssue
			// or nil; this branch is defensive in case that contract is
			// ever violated, so a stray plain error still can't abort the
			// loop or get silently dropped.
			issues = append(issues, newModuleIssue(name, "unexpected error: %v", refreshErr))
		}
	}

	return issues, nil
}
