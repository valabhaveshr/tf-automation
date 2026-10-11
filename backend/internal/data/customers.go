package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const customerColumns = `id, name, business_short_code, created_at, updated_at,
	contact_name, contact_email, contact_phone, country, industry,
	default_cloud, default_region, gcp_organization_id, billing_account_id`

func scanCustomer(row rowScanner, c *Customer) error {
	return row.Scan(&c.ID, &c.Name, &c.BusinessShortCode, &c.CreatedAt, &c.UpdatedAt,
		&c.ContactName, &c.ContactEmail, &c.ContactPhone, &c.Country, &c.Industry,
		&c.DefaultCloud, &c.DefaultRegion, &c.GCPOrganizationID, &c.BillingAccountID)
}

// CreateCustomer registers a new customer with its business_short_code. A
// duplicate name is returned as a *ConflictError (maps to the OpenAPI
// POST /admin/customers 409).
func (s *Store) CreateCustomer(ctx context.Context, c *Customer) (*Customer, error) {
	row := s.pool.QueryRow(ctx, insertCustomerSQL, customerInsertArgs(c)...)
	out := &Customer{}
	if err := scanCustomer(row, out); err != nil {
		return nil, fmt.Errorf("data: create customer: %w", wrapConflict(err))
	}
	return out, nil
}

// GetCustomer fetches a customer by ID.
func (s *Store) GetCustomer(ctx context.Context, id uuid.UUID) (*Customer, error) {
	q := `SELECT ` + customerColumns + ` FROM customers WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	out := &Customer{}
	if err := scanCustomer(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get customer: %w", err)
	}
	return out, nil
}

// GetCustomerByName fetches a customer by its unique name — used to
// resolve an existing customer's business_short_code when a record's
// customer_name is reused across submissions.
func (s *Store) GetCustomerByName(ctx context.Context, name string) (*Customer, error) {
	q := `SELECT ` + customerColumns + ` FROM customers WHERE name = $1`
	row := s.pool.QueryRow(ctx, q, name)
	out := &Customer{}
	if err := scanCustomer(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get customer by name: %w", err)
	}
	return out, nil
}

// ListCustomers returns every customer, ordered by name.
func (s *Store) ListCustomers(ctx context.Context) ([]*Customer, error) {
	q := `SELECT ` + customerColumns + ` FROM customers ORDER BY name`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("data: list customers: %w", err)
	}
	defer rows.Close()

	var out []*Customer
	for rows.Next() {
		c := &Customer{}
		if err := scanCustomer(rows, c); err != nil {
			return nil, fmt.Errorf("data: list customers: scan: %w", err)
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list customers: %w", err)
	}
	return out, nil
}

// insertCustomerSQL/customerInsertArgs are shared by CreateCustomer and
// CompleteOnboarding (which inserts inside its own transaction).
const insertCustomerSQL = `INSERT INTO customers (name, business_short_code,
		contact_name, contact_email, contact_phone, country, industry,
		default_cloud, default_region, gcp_organization_id, billing_account_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, COALESCE(NULLIF($8, ''), 'gcp'), $9, $10, $11)
	RETURNING ` + customerColumns

func customerInsertArgs(c *Customer) []any {
	return []any{c.Name, c.BusinessShortCode,
		c.ContactName, c.ContactEmail, c.ContactPhone, c.Country, c.Industry,
		c.DefaultCloud, c.DefaultRegion, c.GCPOrganizationID, c.BillingAccountID}
}

// UpdateCustomer updates a customer's name, business_short_code (admin-
// editable per the locked decision) and profile fields. Callers decide who
// may change which field; this writes them all.
func (s *Store) UpdateCustomer(ctx context.Context, c *Customer) (*Customer, error) {
	q := `UPDATE customers SET name = $1, business_short_code = $2,
		contact_name = $3, contact_email = $4, contact_phone = $5, country = $6, industry = $7,
		default_cloud = COALESCE(NULLIF($8, ''), 'gcp'), default_region = $9,
		gcp_organization_id = $10, billing_account_id = $11, updated_at = now()
		WHERE id = $12
		RETURNING ` + customerColumns
	row := s.pool.QueryRow(ctx, q, append(customerInsertArgs(c), c.ID)...)
	out := &Customer{}
	if err := scanCustomer(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: update customer: %w", wrapConflict(err))
	}
	return out, nil
}

// DeleteCustomer hard-deletes a customer row. Note: records.customer_id
// has no ON DELETE behavior specified (defaults to RESTRICT), so deleting
// a customer referenced by any record will fail at the DB level rather
// than silently orphaning records.
func (s *Store) DeleteCustomer(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM customers WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("data: delete customer: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
