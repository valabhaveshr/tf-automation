package api

import (
	"testing"
	"time"
)

func validRecordInput() recordInput {
	return recordInput{
		CustomerName: "acme",
		ProjectName:  "web",
		EnvName:      "prod",
		ModuleSelections: []moduleInstanceInput{
			{ModuleName: "vm-instance", Instances: map[string]map[string]any{"web-01": {}}},
		},
	}
}

func TestValidateRecordInputShape_Valid(t *testing.T) {
	if err := validateRecordInputShape(validRecordInput()); err != nil {
		t.Errorf("expected valid input to pass, got %v", err)
	}
}

func TestValidateRecordInputShape_MissingFields(t *testing.T) {
	missingCustomer := validRecordInput()
	missingCustomer.CustomerName = ""
	if err := validateRecordInputShape(missingCustomer); err == nil {
		t.Error("expected error for missing customer_name")
	}

	noModules := validRecordInput()
	noModules.ModuleSelections = nil
	if err := validateRecordInputShape(noModules); err == nil {
		t.Error("expected error for zero module selections")
	}

	noInstances := validRecordInput()
	noInstances.ModuleSelections[0].Instances = nil
	if err := validateRecordInputShape(noInstances); err == nil {
		t.Error("expected error for zero instances in a module selection")
	}
}

func TestValidateRecordInputShape_DuplicateModule(t *testing.T) {
	dup := recordInput{
		CustomerName: "acme",
		ProjectName:  "web",
		EnvName:      "prod",
		ModuleSelections: []moduleInstanceInput{
			{ModuleName: "vm-instance", Instances: map[string]map[string]any{"a": {}}},
			{ModuleName: "vm-instance", Instances: map[string]map[string]any{"b": {}}},
		},
	}
	if err := validateRecordInputShape(dup); err == nil {
		t.Error("expected error for duplicate module selection")
	}
}

func TestParseIfMatch(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	raw := now.Format(time.RFC3339Nano)

	got, err := parseIfMatch(raw)
	if err != nil {
		t.Fatalf("parseIfMatch(%q): %v", raw, err)
	}
	if !got.Equal(now) {
		t.Errorf("parseIfMatch(%q) = %v, want %v", raw, got, now)
	}

	if _, err := parseIfMatch("not-a-timestamp"); err == nil {
		t.Error("expected an error for a malformed If-Match header")
	}
}

func TestIsEmptyValue(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{nil, true},
		{"", true},
		{"web-01", false},
		{0, false},
		{false, false},
	}
	for _, c := range cases {
		if got := isEmptyValue(c.v); got != c.want {
			t.Errorf("isEmptyValue(%#v) = %v, want %v", c.v, got, c.want)
		}
	}
}
