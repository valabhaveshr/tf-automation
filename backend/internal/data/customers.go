package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const customerColumns = `id, name, business_short_code, created_at, updated_at`

func scanCustomer(row rowScanner, c *Customer) error {
	return row.Scan(&c.ID, &c.Name, &c.BusinessShortCode, &c.CreatedAt, &c.UpdatedAt)
}

// CreateCustomer registers a new customer with its business_short_code. A
// duplicate name is returned as a *ConflictError (maps to the OpenAPI
// POST /admin/customers 409).
func (s *Store) CreateCustomer(ctx context.Context, c *Customer) (*Customer, error) {
	q := `INSERT INTO customers (name, business_short_code) VALUES ($1, $2)
		RETURNING ` + customerColumns
	row := s.pool.QueryRow(ctx, q, c.Name, c.BusinessShortCode)
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

// UpdateCustomer updates a customer's business_short_code (admin-editable
// per the locked decision).
func (s *Store) UpdateCustomer(ctx context.Context, c *Customer) (*Customer, error) {
	q := `UPDATE customers SET business_short_code = $1, updated_at = now() WHERE id = $2
		RETURNING ` + customerColumns
	row := s.pool.QueryRow(ctx, q, c.BusinessShortCode, c.ID)
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
