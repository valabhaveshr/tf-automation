package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// GetUserRoles returns the names of every role assigned to userID, via the
// user_roles join table. Returns a nil slice (not an error) if the user has
// no roles assigned.
func (s *Store) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]string, error) {
	q := `SELECT r.name FROM roles r
		JOIN user_roles ur ON ur.role_id = r.id
		WHERE ur.user_id = $1
		ORDER BY r.name`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("data: get user roles: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("data: get user roles: scan: %w", err)
		}
		out = append(out, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: get user roles: %w", err)
	}
	return out, nil
}

// AssignRole grants userID membership in roleName. Idempotent: assigning a
// role the user already holds is a no-op. Returns ErrNotFound (wrapped) if
// roleName isn't one of the fixed, seeded roles (see migration 000002).
func (s *Store) AssignRole(ctx context.Context, userID uuid.UUID, roleName string) error {
	roleID, err := s.roleIDByName(ctx, roleName)
	if err != nil {
		return fmt.Errorf("data: assign role: %w", err)
	}
	q := `INSERT INTO user_roles (user_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`
	if _, err := s.pool.Exec(ctx, q, userID, roleID); err != nil {
		return fmt.Errorf("data: assign role: %w", err)
	}
	return nil
}

// RevokeRole removes userID's membership in roleName, if it exists.
// Idempotent: revoking a role the user doesn't hold (or that doesn't exist)
// is a no-op rather than an error.
func (s *Store) RevokeRole(ctx context.Context, userID uuid.UUID, roleName string) error {
	q := `DELETE FROM user_roles ur
		USING roles r
		WHERE ur.role_id = r.id AND ur.user_id = $1 AND r.name = $2`
	if _, err := s.pool.Exec(ctx, q, userID, roleName); err != nil {
		return fmt.Errorf("data: revoke role: %w", err)
	}
	return nil
}

// HasRole reports whether userID currently holds roleName. Convenience
// method for authorization middleware; false (not an error) if either the
// user or the role doesn't exist.
func (s *Store) HasRole(ctx context.Context, userID uuid.UUID, roleName string) (bool, error) {
	q := `SELECT EXISTS (
		SELECT 1 FROM user_roles ur
		JOIN roles r ON r.id = ur.role_id
		WHERE ur.user_id = $1 AND r.name = $2
	)`
	var exists bool
	if err := s.pool.QueryRow(ctx, q, userID, roleName).Scan(&exists); err != nil {
		return false, fmt.Errorf("data: has role: %w", err)
	}
	return exists, nil
}

// roleIDByName resolves a role name to its id. Returns ErrNotFound if no
// such role is seeded.
func (s *Store) roleIDByName(ctx context.Context, roleName string) (uuid.UUID, error) {
	var id uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM roles WHERE name = $1`, roleName).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNotFound
		}
		return uuid.Nil, err
	}
	return id, nil
}
