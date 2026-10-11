package catalog

import "testing"

func TestParseMetadataFile_Valid(t *testing.T) {
	src := []byte(`
name: vm-instance
display_name: "Virtual Machine Instance"
category: "Compute"
cloud: "azure"
icon: "server"
description: "Provisions a single VM."
instance_label_field: "name"
naming_resource_type: "azure_vm"
naming_target_variable: "name"
`)

	md, err := ParseMetadataFile("vm-instance", src)
	if err != nil {
		t.Fatalf("ParseMetadataFile returned unexpected error: %v", err)
	}
	if md.Name != "vm-instance" {
		t.Errorf("Name = %q, want %q", md.Name, "vm-instance")
	}
	if md.DisplayName != "Virtual Machine Instance" {
		t.Errorf("DisplayName = %q", md.DisplayName)
	}
	if md.Cloud != "azure" {
		t.Errorf("Cloud = %q", md.Cloud)
	}
	if md.NamingResourceType != "azure_vm" {
		t.Errorf("NamingResourceType = %q", md.NamingResourceType)
	}
}

func TestParseMetadataFile_OptionalFieldsOmitted(t *testing.T) {
	src := []byte(`
name: storage-bucket
display_name: "Storage Bucket"
category: "Storage"
cloud: "gcp"
icon: "bucket"
description: "A bucket."
instance_label_field: "name"
`)

	md, err := ParseMetadataFile("storage-bucket", src)
	if err != nil {
		t.Fatalf("ParseMetadataFile returned unexpected error: %v", err)
	}
	if md.NamingResourceType != "" {
		t.Errorf("NamingResourceType = %q, want empty (opt-in field, not declared)", md.NamingResourceType)
	}
	if md.NamingTargetVariable != "" {
		t.Errorf("NamingTargetVariable = %q, want empty", md.NamingTargetVariable)
	}
}

func TestParseMetadataFile_NameMismatch(t *testing.T) {
	src := []byte(`
name: wrong-name
display_name: "Something"
category: "Compute"
cloud: "aws"
icon: "server"
description: "desc"
instance_label_field: "name"
`)

	_, err := ParseMetadataFile("vm-instance", src)
	if err == nil {
		t.Fatalf("expected an error for a metadata.yaml name that doesn't match the folder name")
	}
}

func TestParseMetadataFile_NestedModuleName(t *testing.T) {
	for _, name := range []string{"instance", "compute/instance"} {
		src := []byte("name: " + name + "\ndisplay_name: \"Instance\"\n")
		if _, err := ParseMetadataFile("compute/instance", src); err != nil {
			t.Errorf("name %q for nested module compute/instance: unexpected error %v", name, err)
		}
	}

	src := []byte("name: compute\ndisplay_name: \"Instance\"\n")
	if _, err := ParseMetadataFile("compute/instance", src); err == nil {
		t.Errorf("expected an error for name %q, which is the parent folder, not the module", "compute")
	}
}

func TestParseMetadataFile_MalformedYAML(t *testing.T) {
	src := []byte("name: [this is not\nvalid: yaml: at: all")

	_, err := ParseMetadataFile("vm-instance", src)
	if err == nil {
		t.Fatalf("expected an error for malformed YAML")
	}
}

func TestParseMetadataFile_VariableHints(t *testing.T) {
	src := []byte(`
name: instance
display_name: "Compute Instance"
variables:
  zone:
    example: "us-central1-a"
    group: "Basics"
  boot_disk_size_gb:
    example: 50
    group: "Boot disk"
  network_tags:
    example: ["web", "allow-ssh"]
    group: "Networking"
  enable_ip_forwarding:
    example: "false"
    group: "Advanced"
    advanced: true
`)
	md, err := ParseMetadataFile("compute/instance", src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := VariableHints{
		{Name: "zone", Example: "us-central1-a", Group: "Basics"},
		{Name: "boot_disk_size_gb", Example: "50", Group: "Boot disk"},
		{Name: "network_tags", Example: `["web", "allow-ssh"]`, Group: "Networking"},
		{Name: "enable_ip_forwarding", Example: "false", Group: "Advanced", Advanced: true},
	}
	if len(md.Variables) != len(want) {
		t.Fatalf("got %d hints, want %d: %+v", len(md.Variables), len(want), md.Variables)
	}
	for i := range want {
		if md.Variables[i] != want[i] {
			t.Errorf("hint %d = %+v, want %+v", i, md.Variables[i], want[i])
		}
	}
}

func TestParseMetadataFile_VariableHintsMustBeMapping(t *testing.T) {
	src := []byte("name: instance\nvariables:\n  - zone\n")
	if _, err := ParseMetadataFile("compute/instance", src); err == nil {
		t.Fatal("expected an error for a variables: list instead of a mapping")
	}
}
