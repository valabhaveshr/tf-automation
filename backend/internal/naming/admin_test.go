package naming

import (
	"context"
	"strings"
	"testing"

	"tf-automation/backend/internal/data"
)

func TestCreateCustomer_RejectsInvalidShortCode(t *testing.T) {
	e, _ := newTestEngine()

	cases := []string{
		"",        // empty
		"ACME",    // uppercase
		"toolong", // >5 chars
		"ac-me",   // hyphen
		"ac me",   // space
	}
	for _, sc := range cases {
		_, err := e.CreateCustomer(context.Background(), "acme corp", sc)
		if err == nil {
			t.Errorf("CreateCustomer(shortCode=%q) = nil error, want error", sc)
		}
	}
}

func TestCreateCustomer_AcceptsValidShortCode(t *testing.T) {
	e, _ := newTestEngine()

	for _, sc := range []string{"a", "acme1", "12345"} {
		cust, err := e.CreateCustomer(context.Background(), "acme corp "+sc, sc)
		if err != nil {
			t.Fatalf("CreateCustomer(shortCode=%q) unexpected error: %v", sc, err)
		}
		if cust.BusinessShortCode != sc {
			t.Errorf("CreateCustomer(shortCode=%q).BusinessShortCode = %q", sc, cust.BusinessShortCode)
		}
	}
}

func TestSaveNamingTemplate_RejectsUnknownToken(t *testing.T) {
	e, _ := newTestEngine()

	_, err := e.SaveNamingTemplate(context.Background(), data.NamingTemplate{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		Template:     "prj-{business_short_code}-{typo_token}-{num_id}",
		NumIDWidth:   2,
	})
	if err == nil {
		t.Fatal("SaveNamingTemplate with unknown token = nil error, want error")
	}
	if !strings.Contains(err.Error(), "typo_token") {
		t.Errorf("error %q should mention the offending token", err.Error())
	}
}

func TestSaveNamingTemplate_PersistsValidTemplate(t *testing.T) {
	e, fs := newTestEngine()

	saved, err := e.SaveNamingTemplate(context.Background(), data.NamingTemplate{
		Cloud:        "aws",
		ResourceType: "aws_vpc",
		Template:     "vpc-{business_short_code}-{env}-{num_id}",
		NumIDWidth:   3,
		CreatedBy:    "admin@example.com",
	})
	if err != nil {
		t.Fatalf("SaveNamingTemplate unexpected error: %v", err)
	}
	if saved.Template != "vpc-{business_short_code}-{env}-{num_id}" {
		t.Errorf("saved template = %q", saved.Template)
	}

	// Confirm it actually landed in the store (delegation happened).
	got, err := fs.GetNamingTemplate(context.Background(), "aws", "aws_vpc")
	if err != nil {
		t.Fatalf("GetNamingTemplate after save: %v", err)
	}
	if got.NumIDWidth != 3 {
		t.Errorf("stored NumIDWidth = %d, want 3", got.NumIDWidth)
	}
}
