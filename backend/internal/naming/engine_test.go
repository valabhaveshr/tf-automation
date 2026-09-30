package naming

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"tf-automation/backend/internal/data"
)

func newTestEngine() (*Engine, *fakeStore) {
	fs := newFakeStore()
	return &Engine{store: fs}, fs
}

func TestSuggest_NoTemplateConfigured(t *testing.T) {
	e, fs := newTestEngine()
	custID := uuid.New()
	fs.putCustomer(&data.Customer{ID: custID, Name: "acme", BusinessShortCode: "acme1"})
	// Deliberately no naming_templates row for (gcp, gcp_project).

	_, err := e.Suggest(context.Background(), NameRequest{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		EnvName:      "prod",
		Project:      "web",
		UserLabel:    "svc",
		CustomerID:   custID,
	})
	if !errors.Is(err, ErrNoTemplateConfigured) {
		t.Fatalf("Suggest() err = %v, want ErrNoTemplateConfigured", err)
	}
}

func TestSuggest_CustomerShortCodeMissing(t *testing.T) {
	e, fs := newTestEngine()
	custID := uuid.New()
	fs.putCustomer(&data.Customer{ID: custID, Name: "acme", BusinessShortCode: ""}) // unset
	fs.putTemplate(&data.NamingTemplate{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		Template:     "prj-{business_short_code}-{env}-{num_id}",
		NumIDWidth:   2,
	})

	_, err := e.Suggest(context.Background(), NameRequest{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		EnvName:      "prod",
		CustomerID:   custID,
	})
	if !errors.Is(err, ErrCustomerShortCodeMissing) {
		t.Fatalf("Suggest() err = %v, want ErrCustomerShortCodeMissing", err)
	}
}

func TestSuggest_DoesNotAllocate(t *testing.T) {
	e, fs := newTestEngine()
	custID := uuid.New()
	fs.putCustomer(&data.Customer{ID: custID, Name: "acme", BusinessShortCode: "acme1"})
	fs.putTemplate(&data.NamingTemplate{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		Template:     "prj-{business_short_code}-{env}-{num_id}",
		NumIDWidth:   2,
	})

	req := NameRequest{Cloud: "gcp", ResourceType: "gcp_project", EnvName: "prod", CustomerID: custID}

	name1, err := e.Suggest(context.Background(), req)
	if err != nil {
		t.Fatalf("first Suggest: unexpected error: %v", err)
	}
	name2, err := e.Suggest(context.Background(), req)
	if err != nil {
		t.Fatalf("second Suggest: unexpected error: %v", err)
	}
	// Suggest must never consume a counter slot, so repeated previews
	// return the SAME tentative name.
	if name1 != name2 {
		t.Errorf("Suggest is not idempotent (it allocated): first=%q second=%q", name1, name2)
	}
	want := "prj-acme1-prod-01"
	if name1 != want {
		t.Errorf("Suggest() = %q, want %q", name1, want)
	}
}

func TestAllocate_Success_IncrementsAcrossCalls(t *testing.T) {
	e, fs := newTestEngine()
	custID := uuid.New()
	fs.putCustomer(&data.Customer{ID: custID, Name: "acme", BusinessShortCode: "acme1"})
	fs.putTemplate(&data.NamingTemplate{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		Template:     "prj-{business_short_code}-{env}-{num_id}",
		NumIDWidth:   2,
	})

	req := NameRequest{Cloud: "gcp", ResourceType: "gcp_project", EnvName: "prod", CustomerID: custID}

	name1, id1, err := e.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("first Allocate: unexpected error: %v", err)
	}
	if id1 != 1 || name1 != "prj-acme1-prod-01" {
		t.Errorf("first Allocate = (%q, %d), want (\"prj-acme1-prod-01\", 1)", name1, id1)
	}

	name2, id2, err := e.Allocate(context.Background(), req)
	if err != nil {
		t.Fatalf("second Allocate: unexpected error: %v", err)
	}
	if id2 != 2 || name2 != "prj-acme1-prod-02" {
		t.Errorf("second Allocate = (%q, %d), want (\"prj-acme1-prod-02\", 2)", name2, id2)
	}
}

func TestAllocate_PropagatesCounterExhaustion(t *testing.T) {
	e, fs := newTestEngine()
	custID := uuid.New()
	fs.putCustomer(&data.Customer{ID: custID, Name: "acme", BusinessShortCode: "acme1"})
	fs.putTemplate(&data.NamingTemplate{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		Template:     "prj-{business_short_code}-{env}-{num_id}",
		NumIDWidth:   1, // max value 9
	})

	req := NameRequest{Cloud: "gcp", ResourceType: "gcp_project", EnvName: "prod", CustomerID: custID}

	// Drain values 1..9.
	for i := 1; i <= 9; i++ {
		if _, _, err := e.Allocate(context.Background(), req); err != nil {
			t.Fatalf("Allocate #%d: unexpected error: %v", i, err)
		}
	}

	// The 10th allocation must exceed width 1 (max 9) and fail with
	// data.ErrCounterExhausted, propagated as-is (not wrapped away).
	_, _, err := e.Allocate(context.Background(), req)
	if !errors.Is(err, data.ErrCounterExhausted) {
		t.Fatalf("Allocate() after exhaustion err = %v, want data.ErrCounterExhausted", err)
	}
}

func TestAllocate_Concurrent_NoDuplicateNumIDs(t *testing.T) {
	// NOTE: this only exercises fakeStore's mutex-guarded concurrency
	// semantics (see fake_store_test.go). Real atomicity depends on
	// WS-A's actual row-locked SQL (INSERT ... ON CONFLICT ... DO UPDATE
	// ... RETURNING) running against Postgres, which is WS-A's own
	// integration test's responsibility, not this package's.
	e, fs := newTestEngine()
	custID := uuid.New()
	fs.putCustomer(&data.Customer{ID: custID, Name: "acme", BusinessShortCode: "acme1"})
	fs.putTemplate(&data.NamingTemplate{
		Cloud:        "gcp",
		ResourceType: "gcp_project",
		Template:     "prj-{business_short_code}-{env}-{num_id}",
		NumIDWidth:   4, // plenty of headroom for N goroutines
	})

	req := NameRequest{Cloud: "gcp", ResourceType: "gcp_project", EnvName: "prod", CustomerID: custID}

	const n = 100
	var wg sync.WaitGroup
	ids := make([]int, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, numID, err := e.Allocate(context.Background(), req)
			ids[idx] = numID
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	seen := make(map[int]bool, n)
	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
		if seen[ids[i]] {
			t.Fatalf("duplicate numID %d allocated (goroutine %d)", ids[i], i)
		}
		seen[ids[i]] = true
	}
	if len(seen) != n {
		t.Fatalf("got %d unique numIDs, want %d", len(seen), n)
	}
}
