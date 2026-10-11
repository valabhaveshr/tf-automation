package api

import "testing"

func TestModuleNameFromSchemaPath(t *testing.T) {
	cases := []struct {
		rest     string
		wantName string
		wantOK   bool
	}{
		{"vm-instance/schema", "vm-instance", true},
		{"compute/instance/schema", "compute/instance", true},
		{"compute%2Finstance/schema", "compute/instance", true},
		{"compute/instance", "", false},
		{"/schema", "", false},
		{"schema", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		gotName, gotOK := moduleNameFromSchemaPath(c.rest)
		if gotName != c.wantName || gotOK != c.wantOK {
			t.Errorf("moduleNameFromSchemaPath(%q) = (%q, %v), want (%q, %v)", c.rest, gotName, gotOK, c.wantName, c.wantOK)
		}
	}
}
