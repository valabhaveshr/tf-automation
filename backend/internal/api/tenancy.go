package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/data"
)

// tenant is the caller's identity and tenancy scope, loaded from the DB per
// request rather than trusted from the session JWT: customer membership
// changes at onboarding, after the cookie was issued.
//
//   - Platform admin: 'admin' role and no customer. Sees every customer's
//     resources and picks the customer when creating one.
//   - Customer user: belongs to exactly one customer and only ever sees or
//     writes that customer's resources. 'customer_admin' additionally
//     manages the customer profile and its users.
//   - Neither (a customer admin before onboarding, or an SSO user nobody
//     invited): can't touch resources until they belong to a customer.
type tenant struct {
	user          *data.User
	roles         []string
	platformAdmin bool
	customerAdmin bool
	customer      *data.Customer // nil when the user has no customer
}

func (s *Server) loadTenant(ctx context.Context, r *http.Request) (*tenant, error) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		return nil, newAPIError(http.StatusUnauthorized, "unauthorized")
	}
	u, err := s.Store.GetUser(ctx, claims.UserID)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return nil, newAPIError(http.StatusUnauthorized, "unauthorized")
		}
		return nil, err
	}
	roles, err := s.Store.GetUserRoles(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	t := &tenant{user: u, roles: roles}
	t.customerAdmin = containsString(roles, data.RoleCustomerAdmin)
	if u.CustomerID != nil {
		c, err := s.Store.GetCustomer(ctx, *u.CustomerID)
		if err != nil {
			return nil, err
		}
		t.customer = c
	} else {
		t.platformAdmin = u.IsAdmin || containsString(roles, data.RoleAdmin)
	}
	return t, nil
}

// needsOnboarding is true for a customer admin who hasn't created their
// customer yet; the frontend routes them to the onboarding form.
func (t *tenant) needsOnboarding() bool {
	return t.customer == nil && !t.platformAdmin && t.customerAdmin
}

func (t *tenant) canAccessRecord(rec *data.Record) bool {
	if t.platformAdmin {
		return true
	}
	return t.customer != nil && rec.CustomerID != nil && *rec.CustomerID == t.customer.ID
}

// recordScope returns the customer to restrict record listings to: the
// caller's own customer, or for a platform admin the optional
// ?customer_id filter (nil = all customers).
func (t *tenant) recordScope(r *http.Request) (*uuid.UUID, error) {
	if !t.platformAdmin {
		if t.customer == nil {
			return nil, errNoCustomer(t)
		}
		id := t.customer.ID
		return &id, nil
	}
	raw := r.URL.Query().Get("customer_id")
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, badRequest("invalid customer_id")
	}
	return &id, nil
}

// customerForWrite picks the customer a new or edited resource belongs to:
// always the caller's own customer for customer users (any customer named
// in the request is ignored), or the requested customer_id for a platform
// admin.
func (s *Server) customerForWrite(ctx context.Context, t *tenant, requested *uuid.UUID) (*data.Customer, error) {
	if t.customer != nil {
		return t.customer, nil
	}
	if !t.platformAdmin {
		return nil, errNoCustomer(t)
	}
	if requested == nil {
		return nil, badRequest("customer_id is required")
	}
	c, err := s.Store.GetCustomer(ctx, *requested)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			return nil, badRequest("unknown customer_id")
		}
		return nil, err
	}
	return c, nil
}

// loadAccessibleRecord fetches a record the caller is allowed to see. A
// record belonging to another customer is reported as not found, so record
// IDs from other tenants aren't confirmed to exist.
func (s *Server) loadAccessibleRecord(ctx context.Context, t *tenant, id uuid.UUID) (*data.Record, error) {
	rec, err := s.Store.GetRecord(ctx, id)
	if err != nil {
		return nil, err
	}
	if !t.canAccessRecord(rec) {
		return nil, data.ErrNotFound
	}
	return rec, nil
}

func errNoCustomer(t *tenant) error {
	if t.needsOnboarding() {
		return forbidden("complete your organization's onboarding first")
	}
	return forbidden("your account isn't linked to a customer yet; ask your administrator for an invitation")
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// toResolvedCustomer adapts a registry customer to the naming engine's
// input shape.
func toResolvedCustomer(c *data.Customer) resolvedCustomer {
	return resolvedCustomer{id: c.ID, businessShortCode: c.BusinessShortCode, found: true}
}

// prepareRecordInput resolves the caller's tenant and the customer the
// request is for, overwrites req.CustomerName with that customer's registry
// name (it drives the output path, commit message and audit detail), then
// validates the request shape. existing is the record being edited, nil
// for a new one; editing keeps the record's own customer.
func (s *Server) prepareRecordInput(ctx context.Context, r *http.Request, req *recordInput, existing *data.Record) (*data.Customer, error) {
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		return nil, err
	}
	requested := req.CustomerID
	if existing != nil {
		if !t.canAccessRecord(existing) {
			return nil, data.ErrNotFound
		}
		if existing.CustomerID != nil {
			requested = existing.CustomerID
		}
	}
	customer, err := s.customerForWrite(ctx, t, requested)
	if err != nil {
		return nil, err
	}
	req.CustomerName = customer.Name
	if err := validateRecordInputShape(*req); err != nil {
		return nil, badRequest(err.Error())
	}
	return customer, nil
}

func toCustomerResponse(c *data.Customer) customerResponse {
	return customerResponse{
		ID:                c.ID,
		Name:              c.Name,
		BusinessShortCode: c.BusinessShortCode,
		ContactName:       c.ContactName,
		ContactEmail:      c.ContactEmail,
		ContactPhone:      c.ContactPhone,
		Country:           c.Country,
		Industry:          c.Industry,
		DefaultCloud:      c.DefaultCloud,
		DefaultRegion:     c.DefaultRegion,
		GCPOrganizationID: c.GCPOrganizationID,
		BillingAccountID:  c.BillingAccountID,
		CreatedAt:         c.CreatedAt,
	}
}
