package codegen

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update regenerates the golden files in testdata/golden from the current
// renderer output, instead of comparing against them. Run once (after `go
// build` succeeds for the first time in an environment where Go is
// actually installed -- this package was authored without one) via:
//
//	go test ./internal/codegen/... -run TestGolden -update
//
// then inspect the diff of testdata/golden before committing it, to
// confirm the regenerated output still matches plan/ARCHITECTURE.md's
// illustrative HCL shape (block/attribute order, file names, etc.) and not
// just "whatever the code happened to emit".
var update = flag.Bool("update", false, "update golden files in testdata/golden")

// moduleFixture mirrors the top-level shape of contracts/var-schema.example.json
// (module_name/version_tag/variables), so tests can decode the local copy at
// testdata/fixtures/vm-instance.varschema.json directly into a ModuleInput.
type moduleFixture struct {
	ModuleName string      `json:"module_name"`
	VersionTag string      `json:"version_tag"`
	Variables  []VarSchema `json:"variables"`
}

func loadModuleFixture(t *testing.T, path string) moduleFixture {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	var mf moduleFixture
	if err := json.Unmarshal(data, &mf); err != nil {
		t.Fatalf("decoding fixture %s: %v", path, err)
	}
	return mf
}

// vmInstanceModule builds a ModuleInput for the "vm-instance" fixture with
// two instances: "web-01" (size omitted -> falls back to its declared
// default) and "web-02" (all fields explicit, including an empty tags
// map).
func vmInstanceModule(t *testing.T) ModuleInput {
	t.Helper()
	mf := loadModuleFixture(t, filepath.Join("testdata", "fixtures", "vm-instance.varschema.json"))
	return ModuleInput{
		ModuleName:    mf.ModuleName,
		PinnedVersion: mf.VersionTag,
		Variables:     mf.Variables,
		Instances: map[string]map[string]any{
			"web-01": {
				"image":          "ubuntu-22.04",
				"admin_password": "S3cr3t!",
				"name":           "prj-acme1-prod-web-01",
				"tags": map[string]any{
					"env": "prod",
				},
			},
			"web-02": {
				"size":           "Standard_B4ms",
				"image":          "ubuntu-22.04",
				"admin_password": "An0ther$ecret",
				"name":           "prj-acme1-prod-web-02",
				"tags":           map[string]any{},
			},
		},
	}
}

// storageBucketModule is a small second, synthetic module (not part of the
// frozen VarSchema fixture) used only to exercise multi-module rendering:
// block/file ordering and independent per-module .auto.tfvars output.
func storageBucketModule() ModuleInput {
	return ModuleInput{
		ModuleName:    "storage-bucket",
		PinnedVersion: "storage-bucket/v0.3.0",
		Variables: []VarSchema{
			{Name: "tier", Type: "string", TypeRaw: "string", HasDefault: true, Default: "standard", Required: false, Nullable: false},
		},
		Instances: map[string]map[string]any{
			"logs": {"tier": "cold"},
		},
	}
}

func writeOrCompareGolden(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		for name, content := range files {
			if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
				t.Fatalf("writing golden %s: %v", name, err)
			}
		}
		return
	}
	for name, got := range files {
		wantPath := filepath.Join(dir, name)
		want, err := os.ReadFile(wantPath)
		if err != nil {
			t.Fatalf("reading golden %s (run with -update to create it): %v", wantPath, err)
		}
		if string(got) != string(want) {
			t.Errorf("golden mismatch for %s\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
		}
	}
}

func TestGolden_SingleModule(t *testing.T) {
	record := RecordInput{
		ModulesRepoOrg: "org",
		Modules:        []ModuleInput{vmInstanceModule(t)},
	}
	files, err := RenderRootFiles(record)
	if err != nil {
		t.Fatalf("RenderRootFiles: %v", err)
	}
	if len(files) != 3 { // variables.tf, main.tf, vm-instance.auto.tfvars
		t.Fatalf("expected 3 output files, got %d: %v", len(files), keysOf(files))
	}
	writeOrCompareGolden(t, filepath.Join("testdata", "golden", "single-module"), files)
}

func TestGolden_MultiModule(t *testing.T) {
	record := RecordInput{
		ModulesRepoOrg: "org",
		Modules:        []ModuleInput{vmInstanceModule(t), storageBucketModule()},
	}
	files, err := RenderRootFiles(record)
	if err != nil {
		t.Fatalf("RenderRootFiles: %v", err)
	}
	if len(files) != 4 { // variables.tf, main.tf, vm-instance.auto.tfvars, storage-bucket.auto.tfvars
		t.Fatalf("expected 4 output files, got %d: %v", len(files), keysOf(files))
	}
	writeOrCompareGolden(t, filepath.Join("testdata", "golden", "multi-module"), files)
}

func TestGolden_BackendFile(t *testing.T) {
	gcs := RenderBackendFile("gcs", "org-tfstate-bucket", "acme/webshop/prod")
	s3 := RenderBackendFile("s3", "org-tfstate-bucket", "acme/webshop/prod")
	writeOrCompareGolden(t, filepath.Join("testdata", "golden", "backend"), map[string][]byte{
		"gcs-backend.tf": gcs,
		"s3-backend.tf":  s3,
	})
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestRenderRootFiles_ZeroModules(t *testing.T) {
	_, err := RenderRootFiles(RecordInput{})
	if err == nil {
		t.Fatal("expected an error for a record with zero module selections, got nil")
	}
}

func TestRenderRootFiles_InvalidInstanceName(t *testing.T) {
	record := RecordInput{
		Modules: []ModuleInput{{
			ModuleName:    "vm-instance",
			PinnedVersion: "vm-instance/v1.2.0",
			Variables:     []VarSchema{{Name: "size", Type: "string", HasDefault: true, Default: "x"}},
			Instances: map[string]map[string]any{
				"Web 01 !!": {"size": "x"}, // invalid: spaces/uppercase/punctuation
			},
		}},
	}
	_, err := RenderRootFiles(record)
	if err == nil {
		t.Fatal("expected an error for an invalid instance name, got nil")
	}
}

func TestRenderRootFiles_MissingRequiredValue(t *testing.T) {
	record := RecordInput{
		Modules: []ModuleInput{{
			ModuleName:    "vm-instance",
			PinnedVersion: "vm-instance/v1.2.0",
			Variables: []VarSchema{
				{Name: "image", Type: "string", HasDefault: false, Required: true, Nullable: false},
			},
			Instances: map[string]map[string]any{
				"web-01": {}, // "image" is required, has no default, not nullable, and is missing
			},
		}},
	}
	_, err := RenderRootFiles(record)
	if err == nil {
		t.Fatal("expected an error for a missing required value, got nil")
	}
}

func TestRenderRootFiles_NestedModuleName(t *testing.T) {
	record := RecordInput{
		Modules: []ModuleInput{{
			ModuleName:    "compute/instance",
			PinnedVersion: "compute/instance/v1.0.0",
			Variables:     []VarSchema{{Name: "size", Type: "string", HasDefault: true, Default: "small"}},
			Instances:     map[string]map[string]any{"web-01": {}},
		}},
		ModulesRepoOrg:  "acme",
		ModulesRepoName: "tf-modules",
		ModulesPath:     "modules",
	}
	files, err := RenderRootFiles(record)
	if err != nil {
		t.Fatalf("RenderRootFiles: %v", err)
	}

	// Slashes in the module name must not create a subdirectory: Terraform
	// only auto-loads *.auto.tfvars from the root module directory.
	tfvars, ok := files["compute-instance.auto.tfvars"]
	if !ok {
		t.Fatalf("expected compute-instance.auto.tfvars, got files %v", keysOf(files))
	}
	if !bytes.Contains(tfvars, []byte("compute_instance_instances")) {
		t.Errorf("tfvars does not set compute_instance_instances:\n%s", tfvars)
	}

	mainTF := string(files["main.tf"])
	for _, want := range []string{
		`module "compute_instance" {`,
		`"git::https://github.com/acme/tf-modules.git//modules/compute/instance?ref=compute/instance/v1.0.0"`,
		`var.compute_instance_instances`,
	} {
		if !strings.Contains(mainTF, want) {
			t.Errorf("main.tf missing %s:\n%s", want, mainTF)
		}
	}
}

func TestRenderRootFiles_ModuleNamesMappingToSameIdentifier(t *testing.T) {
	mod := func(name string) ModuleInput {
		return ModuleInput{
			ModuleName:    name,
			PinnedVersion: name + "/v1.0.0",
			Variables:     []VarSchema{{Name: "size", Type: "string", HasDefault: true, Default: "x"}},
			Instances:     map[string]map[string]any{"a": {}},
		}
	}
	// Both map to identifier "compute_instance" and file stem
	// "compute-instance"; rendering both would silently clobber one.
	_, err := RenderRootFiles(RecordInput{Modules: []ModuleInput{mod("compute/instance"), mod("compute-instance")}})
	if err == nil {
		t.Fatal("expected an error for two module names mapping to the same identifier, got nil")
	}
}

func TestRenderRootFiles_InvalidModulePath(t *testing.T) {
	for _, name := range []string{"compute/../secrets", "/compute", "compute/", "9lives", "compute instance"} {
		_, err := RenderRootFiles(RecordInput{Modules: []ModuleInput{{
			ModuleName:    name,
			PinnedVersion: "x/v1.0.0",
			Variables:     []VarSchema{{Name: "size", Type: "string", HasDefault: true, Default: "x"}},
			Instances:     map[string]map[string]any{"a": {}},
		}}})
		if err == nil {
			t.Errorf("module name %q: expected an error, got nil", name)
		}
	}
}
