package naming

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"tf-automation/backend/internal/data"
)

// Sentinel errors returned by Suggest and Allocate. Callers (WS-G) map
// these to the specific handling required by plan/ARCHITECTURE.md's
// "Naming convention" edge-case table.
var (
	// ErrNoTemplateConfigured is returned when no naming_templates row
	// exists for the requested (Cloud, ResourceType). Per the edge case
	// table, this is NOT a hard failure: the caller must map it to the
	// "no naming template configured for this resource type" fallback
	// message and still allow module selection / a plain editable name
	// field, rather than blocking.
	ErrNoTemplateConfigured = errors.New("naming: no naming template configured for this resource type")

	// ErrCustomerShortCodeMissing is returned when the customer resolved
	// from CustomerID has an empty/unset business_short_code. Per the
	// edge case table, this DOES block: the caller must surface "this
	// customer needs a business short code configured" before the naming
	// engine is invoked further, resolved via the admin customers
	// registry endpoint.
	ErrCustomerShortCodeMissing = errors.New("naming: customer has no business_short_code configured")
)

// NameRequest is the input to Suggest and Allocate: everything needed to
// resolve a naming_templates row and render a compliant name.
type NameRequest struct {
	Cloud        string
	ResourceType string
	EnvName      string
	Project      string
	UserLabel    string
	CustomerID   uuid.UUID
}

// Store is the subset of WS-A's *data.Store the Naming Convention Engine
// depends on. *data.Store satisfies this interface; tests fake it
// in-package (see engine_test.go) so this package never needs a real
// Postgres connection to test its own logic. WS-A's
// AllocateNamingCounter and PeekNamingCounter implementations are used
// as-is here — this package never reimplements the atomic row-locked
// increment or the exhaustion check.
type Store interface {
	GetNamingTemplate(ctx context.Context, cloud, resourceType string) (*data.NamingTemplate, error)
	UpsertNamingTemplate(ctx context.Context, t *data.NamingTemplate) (*data.NamingTemplate, error)
	GetCustomer(ctx context.Context, id uuid.UUID) (*data.Customer, error)
	CreateCustomer(ctx context.Context, c *data.Customer) (*data.Customer, error)
	PeekNamingCounter(ctx context.Context, businessShortCode, envName, resourceType string) (int, error)
	AllocateNamingCounter(ctx context.Context, businessShortCode, envName, resourceType string, width int) (int, error)
}

// Engine is WS-I's Naming Convention Engine, wrapping WS-A's data.Store.
type Engine struct {
	store Store
}

// NewEngine builds an Engine over WS-A's real *data.Store.
func NewEngine(store *data.Store) *Engine {
	return &Engine{store: store}
}

// resolve is the lookup logic shared by Suggest and Allocate: find the
// naming_templates row for (req.Cloud, req.ResourceType), then the
// customer's business_short_code. Returns the specific sentinel errors
// documented on ErrNoTemplateConfigured / ErrCustomerShortCodeMissing.
func (e *Engine) resolve(ctx context.Context, req NameRequest) (tmpl *data.NamingTemplate, businessShortCode string, err error) {
	tmpl, err = e.store.GetNamingTemplate(ctx, req.Cloud, req.ResourceType)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return nil, "", ErrNoTemplateConfigured
		}
		return nil, "", fmt.Errorf("naming: lookup naming template: %w", err)
	}

	cust, err := e.store.GetCustomer(ctx, req.CustomerID)
	if err != nil {
		return nil, "", fmt.Errorf("naming: lookup customer: %w", err)
	}
	if cust.BusinessShortCode == "" {
		return nil, "", ErrCustomerShortCodeMissing
	}

	return tmpl, cust.BusinessShortCode, nil
}

func renderValues(businessShortCode string, req NameRequest) map[string]string {
	return map[string]string{
		"business_short_code": businessShortCode,
		"env":                 req.EnvName,
		"project":             req.Project,
		"user_label":          req.UserLabel,
	}
}

// Suggest computes a TENTATIVE, non-allocating preview name for req. It
// never calls AllocateNamingCounter — per the locked decision
// ("allocation happens atomically at save time, not preview"), a preview
// must not burn a counter slot. Instead it uses PeekNamingCounter's
// current next_value as the tentative num_id.
//
// Returns ErrNoTemplateConfigured if no naming_templates row matches
// (req.Cloud, req.ResourceType) — the caller (WS-G) must map this to the
// "no naming template configured" fallback message, not a hard failure.
// Returns ErrCustomerShortCodeMissing if the customer has no
// business_short_code set.
func (e *Engine) Suggest(ctx context.Context, req NameRequest) (string, error) {
	tmpl, businessShortCode, err := e.resolve(ctx, req)
	if err != nil {
		return "", err
	}

	nextValue, err := e.store.PeekNamingCounter(ctx, businessShortCode, req.EnvName, req.ResourceType)
	if err != nil {
		return "", fmt.Errorf("naming: peek naming counter: %w", err)
	}

	name, err := RenderTemplate(tmpl.Template, renderValues(businessShortCode, req), nextValue, tmpl.NumIDWidth)
	if err != nil {
		return "", fmt.Errorf("naming: render preview: %w", err)
	}
	return name, nil
}

// Allocate computes the REAL, save-time name for req. Unlike Suggest,
// this calls WS-A's AllocateNamingCounter, which performs the atomic
// row-locked increment and returns a data.ErrCounterExhausted-wrapping
// error if the configured width would be exceeded. That error is
// propagated as-is (not wrapped away) so callers can errors.Is-match it.
// This is the ONLY naming.Engine path that should ever consume a counter
// slot — callers must never call Allocate from a preview code path.
func (e *Engine) Allocate(ctx context.Context, req NameRequest) (name string, numID int, err error) {
	tmpl, businessShortCode, err := e.resolve(ctx, req)
	if err != nil {
		return "", 0, err
	}

	numID, err = e.store.AllocateNamingCounter(ctx, businessShortCode, req.EnvName, req.ResourceType, tmpl.NumIDWidth)
	if err != nil {
		// Propagate as-is: this may wrap data.ErrCounterExhausted, which
		// the caller maps to "save rejected, naming the exhausted
		// counter" per the edge case table.
		return "", 0, err
	}

	name, err = RenderTemplate(tmpl.Template, renderValues(businessShortCode, req), numID, tmpl.NumIDWidth)
	if err != nil {
		return "", 0, fmt.Errorf("naming: render: %w", err)
	}
	return name, numID, nil
}
