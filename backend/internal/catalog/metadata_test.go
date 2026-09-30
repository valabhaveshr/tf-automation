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

func TestParseMetadataFile_MalformedYAML(t *testing.T) {
	src := []byte("name: [this is not\nvalid: yaml: at: all")

	_, err := ParseMetadataFile("vm-instance", src)
	if err == nil {
		t.Fatalf("expected an error for malformed YAML")
	}
}
