package naming

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"

	"tf-automation/backend/internal/data"
)

// fakeStore is a minimal, mutex-guarded in-memory implementation of the
// Store interface, used by this package's own tests so they never need a
// real Postgres connection. It intentionally mirrors the *observable*
// behavior of WS-A's real backend.internal/data.Store (see
// backend/internal/data/naming_counters.go, customers.go,
// naming_templates.go):
//   - GetNamingTemplate / GetCustomer return data.ErrNotFound when missing.
//   - AllocateNamingCounter increments atomically (guarded here by a Go
//     mutex instead of a Postgres row lock) and returns a
//     data.ErrCounterExhausted-wrapping error, WITHOUT incrementing,
//     once the next value would exceed 10^width - 1.
//   - PeekNamingCounter returns the same "next value" without mutating
//     anything, matching data.Store.PeekNamingCounter's contract.
//
// NOTE: the concurrency test in engine_test.go (TestAllocate_Concurrent...)
// only proves that THIS fake's mutex-guarded map serializes correctly. It
// is not a substitute for a real-Postgres test of WS-A's actual
// row-locked SQL (INSERT ... ON CONFLICT ... DO UPDATE ... RETURNING) —
// that atomicity guarantee is WS-A's to verify against a live database.
type fakeStore struct {
	mu        sync.Mutex
	templates map[string]*data.NamingTemplate
	customers map[uuid.UUID]*data.Customer
	counters  map[string]int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		templates: map[string]*data.NamingTemplate{},
		customers: map[uuid.UUID]*data.Customer{},
		counters:  map[string]int{},
	}
}

func templateKey(cloud, resourceType string) string {
	return cloud + "|" + resourceType
}

func counterKey(businessShortCode, envName, resourceType string) string {
	return businessShortCode + "|" + envName + "|" + resourceType
}

func (f *fakeStore) putTemplate(t *data.NamingTemplate) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.templates[templateKey(t.Cloud, t.ResourceType)] = &cp
}

func (f *fakeStore) putCustomer(c *data.Customer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *c
	f.customers[c.ID] = &cp
}

func (f *fakeStore) GetNamingTemplate(_ context.Context, cloud, resourceType string) (*data.NamingTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.templates[templateKey(cloud, resourceType)]
	if !ok {
		return nil, data.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeStore) UpsertNamingTemplate(_ context.Context, t *data.NamingTemplate) (*data.NamingTemplate, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *t
	f.templates[templateKey(t.Cloud, t.ResourceType)] = &cp
	out := cp
	return &out, nil
}

func (f *fakeStore) GetCustomer(_ context.Context, id uuid.UUID) (*data.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.customers[id]
	if !ok {
		return nil, data.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeStore) CreateCustomer(_ context.Context, c *data.Customer) (*data.Customer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	cp := *c
	f.customers[c.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakeStore) PeekNamingCounter(_ context.Context, businessShortCode, envName, resourceType string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.counters[counterKey(businessShortCode, envName, resourceType)]
	if !ok {
		return 1, nil
	}
	return v, nil
}

func (f *fakeStore) AllocateNamingCounter(_ context.Context, businessShortCode, envName, resourceType string, width int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	key := counterKey(businessShortCode, envName, resourceType)
	cur, ok := f.counters[key]
	if !ok {
		cur = 1
	}

	maxValue := 1
	for i := 0; i < width; i++ {
		maxValue *= 10
	}
	maxValue--

	if cur > maxValue {
		return 0, fmt.Errorf("fakeStore: %w", data.ErrCounterExhausted)
	}

	f.counters[key] = cur + 1
	return cur, nil
}
