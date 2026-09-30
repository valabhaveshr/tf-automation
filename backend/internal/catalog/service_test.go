package catalog

import (
	"context"
	"testing"
)

const validVarsTF = `
variable "size" {
  type        = string
  default     = "small"
  description = "instance size"
}
`

func validMetadataYAML(name string) string {
	return "name: " + name + "\n" +
		"display_name: \"" + name + "\"\n" +
		"category: \"Compute\"\n" +
		"cloud: \"aws\"\n" +
		"icon: \"server\"\n" +
		"description: \"a test module\"\n" +
		"instance_label_field: \"name\"\n"
}

func TestRefreshAll_SuccessfulMultiModuleRefresh(t *testing.T) {
	src := newFakeSource()

	src.addTag("module-a/v1.0.0")
	src.addFile("module-a/v1.0.0", "modules/module-a/variables.tf", validVarsTF)
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))

	src.addTag("module-b/v2.3.1")
	src.addFile("module-b/v2.3.1", "modules/module-b/variables.tf", validVarsTF)
	src.addFile("module-b/v2.3.1", "modules/module-b/metadata.yaml", validMetadataYAML("module-b"))

	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	issues, err := svc.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll returned unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("RefreshAll returned unexpected issues: %+v", issues)
	}
	if store.count() != 2 {
		t.Fatalf("expected 2 cataloged module rows, got %d", store.count())
	}
	if row := store.latest("module-a"); row == nil || row.VersionTag != "module-a/v1.0.0" {
		t.Fatalf("module-a not cataloged as expected, got %+v", row)
	}
	if row := store.latest("module-b"); row == nil || row.VersionTag != "module-b/v2.3.1" {
		t.Fatalf("module-b not cataloged as expected, got %+v", row)
	}
}

func TestRefreshAll_PinsHighestSemverTag(t *testing.T) {
	src := newFakeSource()
	src.addTag("module-a/v1.0.0")
	src.addTag("module-a/v1.2.0")
	src.addTag("module-a/v1.10.0") // must sort numerically above v1.2.0, not lexically
	for _, tag := range []string{"module-a/v1.0.0", "module-a/v1.2.0", "module-a/v1.10.0"} {
		src.addFile(tag, "modules/module-a/variables.tf", validVarsTF)
		src.addFile(tag, "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))
	}

	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	if _, err := svc.RefreshAll(context.Background()); err != nil {
		t.Fatalf("RefreshAll returned unexpected error: %v", err)
	}

	row := store.latest("module-a")
	if row == nil {
		t.Fatalf("module-a not cataloged")
	}
	if row.VersionTag != "module-a/v1.10.0" {
		t.Fatalf("expected highest-semver tag module-a/v1.10.0, got %s", row.VersionTag)
	}
}

func TestRefreshAll_UntaggedModuleExcluded(t *testing.T) {
	src := newFakeSource()

	// module-a is properly tagged.
	src.addTag("module-a/v1.0.0")
	src.addFile("module-a/v1.0.0", "modules/module-a/variables.tf", validVarsTF)
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))

	// module-b has files present (e.g. on the default branch) but no
	// <module-name>/vX.Y.Z tag -- per "Module has commits but no tag yet ->
	// Excluded from the catalog", it must never surface in the catalog, and
	// since RefreshAll only discovers module names from valid tags, it's
	// never even attempted (no issue recorded for it either).
	src.addFile("main", "modules/module-b/variables.tf", validVarsTF)
	src.addFile("main", "modules/module-b/metadata.yaml", validMetadataYAML("module-b"))

	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	issues, err := svc.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll returned unexpected error: %v", err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected no issues (untagged module is silently excluded, not flagged), got %+v", issues)
	}
	if store.count() != 1 {
		t.Fatalf("expected exactly 1 cataloged module (module-a only), got %d", store.count())
	}
	if store.latest("module-b") != nil {
		t.Fatalf("module-b should not have been cataloged (no tag)")
	}
}

func TestRefreshModule_UntaggedModuleReturnsIssue(t *testing.T) {
	// Calling RefreshModule directly (e.g. from the webhook's targeted path)
	// for a module with no tag at all must still come back as a
	// *ModuleIssue, not silently succeed or panic.
	src := newFakeSource()
	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	err := svc.RefreshModule(context.Background(), "ghost-module")
	if err == nil {
		t.Fatalf("expected an error for an untagged module")
	}
	issue, ok := err.(*ModuleIssue)
	if !ok {
		t.Fatalf("expected *ModuleIssue, got %T: %v", err, err)
	}
	if issue.ModuleName != "ghost-module" {
		t.Errorf("issue.ModuleName = %q, want %q", issue.ModuleName, "ghost-module")
	}
}

func TestRefreshAll_NameMismatchFlaggedNotFatal(t *testing.T) {
	src := newFakeSource()

	// module-a is fine.
	src.addTag("module-a/v1.0.0")
	src.addFile("module-a/v1.0.0", "modules/module-a/variables.tf", validVarsTF)
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))

	// module-c's metadata.yaml declares a name that doesn't match its own
	// folder/tag-derived name.
	src.addTag("module-c/v1.0.0")
	src.addFile("module-c/v1.0.0", "modules/module-c/variables.tf", validVarsTF)
	src.addFile("module-c/v1.0.0", "modules/module-c/metadata.yaml", validMetadataYAML("wrong-name"))

	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	issues, err := svc.RefreshAll(context.Background())
	if err != nil {
		t.Fatalf("RefreshAll returned unexpected top-level error: %v", err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected exactly 1 issue (module-c), got %d: %+v", len(issues), issues)
	}
	if issues[0].ModuleName != "module-c" {
		t.Errorf("issue.ModuleName = %q, want %q", issues[0].ModuleName, "module-c")
	}

	// module-a must still have cataloged successfully -- the mismatch on
	// module-c must not have aborted the rest of the refresh.
	if row := store.latest("module-a"); row == nil {
		t.Fatalf("module-a should still be cataloged despite module-c's issue")
	}
	if store.latest("module-c") != nil {
		t.Fatalf("module-c should not be cataloged (name mismatch)")
	}
}

func TestRefreshModule_MissingVariablesFile(t *testing.T) {
	src := newFakeSource()
	src.addTag("module-a/v1.0.0")
	// variables.tf deliberately not added.
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))

	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	err := svc.RefreshModule(context.Background(), "module-a")
	if err == nil {
		t.Fatalf("expected an error for a missing variables.tf")
	}
	if _, ok := err.(*ModuleIssue); !ok {
		t.Fatalf("expected *ModuleIssue, got %T: %v", err, err)
	}
	if store.count() != 0 {
		t.Fatalf("expected nothing cataloged, got %d rows", store.count())
	}
}

func TestRefreshModule_MalformedVariablesFileIsFatalToTheModuleOnly(t *testing.T) {
	src := newFakeSource()
	src.addTag("module-a/v1.0.0")
	src.addFile("module-a/v1.0.0", "modules/module-a/variables.tf", `variable "size" { this is not valid HCL `)
	src.addFile("module-a/v1.0.0", "modules/module-a/metadata.yaml", validMetadataYAML("module-a"))

	store := newFakeSchemaStore()
	svc := &Service{Source: src, Store: store, ModulesPath: "modules"}

	err := svc.RefreshModule(context.Background(), "module-a")
	if err == nil {
		t.Fatalf("expected an error for a syntactically invalid variables.tf")
	}
	if _, ok := err.(*ModuleIssue); !ok {
		t.Fatalf("expected *ModuleIssue, got %T: %v", err, err)
	}
	if store.count() != 0 {
		t.Fatalf("expected nothing cataloged for the invalid module, got %d rows", store.count())
	}
}

func TestParseTagName(t *testing.T) {
	tests := []struct {
		tag         string
		wantModule  string
		wantVersion string
		wantOK      bool
	}{
		{"vm-instance/v1.2.0", "vm-instance", "v1.2.0", true},
		{"storage-bucket/v0.0.1", "storage-bucket", "v0.0.1", true},
		{"nested/path/module-name/v1.0.0", "nested/path/module-name", "v1.0.0", true},
		{"v1.2.0", "", "", false},                    // no module name segment at all
		{"vm-instance/v1.2", "", "", false},           // missing patch
		{"vm-instance/1.2.0", "", "", false},          // missing leading "v"
		{"vm-instance/v1.2.0-beta", "", "", false},    // pre-release metadata not supported
		{"vm-instance/v1.2.0+build5", "", "", false},  // build metadata not supported
		{"random-release-tag", "", "", false},         // not a module tag at all
		{"", "", "", false},                           // empty
		{"vm-instance/", "", "", false},                // trailing slash, no version
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			gotModule, gotVersion, gotOK := parseTagName(tt.tag)
			if gotOK != tt.wantOK {
				t.Fatalf("parseTagName(%q) ok = %v, want %v", tt.tag, gotOK, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotModule != tt.wantModule {
				t.Errorf("parseTagName(%q) module = %q, want %q", tt.tag, gotModule, tt.wantModule)
			}
			if gotVersion != tt.wantVersion {
				t.Errorf("parseTagName(%q) version = %q, want %q", tt.tag, gotVersion, tt.wantVersion)
			}
		})
	}
}

func TestHighestTagForModule(t *testing.T) {
	tags := []string{
		"module-a/v1.0.0",
		"module-a/v1.9.0",
		"module-a/v1.10.0",
		"module-a/v2.0.0",
		"module-b/v5.0.0",
		"not-a-module-tag",
	}

	tag, version, ok := highestTagForModule(tags, "module-a")
	if !ok {
		t.Fatalf("expected a highest tag for module-a")
	}
	if tag != "module-a/v2.0.0" || version != "v2.0.0" {
		t.Errorf("got tag=%q version=%q, want tag=%q version=%q", tag, version, "module-a/v2.0.0", "v2.0.0")
	}

	if _, _, ok := highestTagForModule(tags, "module-nonexistent"); ok {
		t.Errorf("expected ok=false for a module with no matching tag")
	}
}
