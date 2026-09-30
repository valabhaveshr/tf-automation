package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// userColumns now includes sso_subject/auth_provider (added by migration
// 000002 but left unwired here until P3-C, per models.go's User doc
// comment) so Roles/AuthProvider/SSOSubject-consuming callers (P3-C's
// UserUpserter, GET /auth/me) get a fully populated *User rather than
// having to re-query. Roles itself is still NOT populated by scanUser --
// it comes from the separate user_roles join table (GetUserRoles), not a
// users-table column, so callers that need it call GetUserRoles
// explicitly.
const userColumns = `id, username, password_hash, is_admin, sso_subject, auth_provider, created_at`

func scanUser(row rowScanner, u *User) error {
	return row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.IsAdmin, &u.SSOSubject, &u.AuthProvider, &u.CreatedAt)
}

// CreateUser inserts a new user. Callers must hash the password themselves
// (WS-F owns password hashing policy); this package only persists whatever
// hash it's given. A duplicate username (or, for an SSO user, a duplicate
// sso_subject) is returned as a *ConflictError. u.AuthProvider defaults to
// "local" when empty; u.SSOSubject may be nil (built-in accounts) or a
// pointer to the IdP's 'sub' claim (SSO-provisioned accounts).
//
// Gap fix (P3-A flagged this, see roles_test.go's prior documentation of
// it): every newly created user previously got no role at all, meaning
// even baseline actions the role model gates would silently fail for them
// until something separately called AssignRole. CreateUser now grants the
// baseline "requester" role immediately, in the same call, for every new
// user -- built-in signup and SSO-provisioned alike (see
// internal/api/oidc_adapter.go's UpsertSSOUser, which creates users via
// this method).
func (s *Store) CreateUser(ctx context.Context, u *User) (*User, error) {
	authProvider := u.AuthProvider
	if authProvider == "" {
		authProvider = "local"
	}
	q := `INSERT INTO users (username, password_hash, is_admin, sso_subject, auth_provider) VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + userColumns
	row := s.pool.QueryRow(ctx, q, u.Username, u.PasswordHash, u.IsAdmin, u.SSOSubject, authProvider)
	out := &User{}
	if err := scanUser(row, out); err != nil {
		return nil, fmt.Errorf("data: create user: %w", wrapConflict(err))
	}

	if err := s.AssignRole(ctx, out.ID, RoleRequester); err != nil {
		return nil, fmt.Errorf("data: create user: assign requester role: %w", err)
	}

	return out, nil
}

// GetUserBySSOSubject fetches a user by their OIDC 'sub' claim -- the
// primary lookup for the SSO login flow (P3-C), since subject (not
// username, which can change at the IdP) is the durable identity key.
// Returns ErrNotFound if no user is linked to that subject yet.
func (s *Store) GetUserBySSOSubject(ctx context.Context, subject string) (*User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE sso_subject = $1`
	row := s.pool.QueryRow(ctx, q, subject)
	out := &User{}
	if err := scanUser(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get user by sso subject: %w", err)
	}
	return out, nil
}

// LinkSSOIdentity binds an existing local (built-in) user to an OIDC
// identity: sets sso_subject and flips auth_provider to "oidc". Used by
// P3-C's UpsertSSOUser to migrate a Phase-1-era built-in account to SSO
// the first time its username matches an IdP login -- the "map built-in
// accounts to IdP identities rather than a hard cutover" mitigation from
// plan/ARCHITECTURE.md's Phase 3 risk table. Returns ErrNotFound if userID
// doesn't exist, or a *ConflictError (via wrapConflict) if subject is
// already linked to a different user (sso_subject is UNIQUE).
func (s *Store) LinkSSOIdentity(ctx context.Context, userID uuid.UUID, subject string) error {
	q := `UPDATE users SET sso_subject = $1, auth_provider = 'oidc' WHERE id = $2`
	tag, err := s.pool.Exec(ctx, q, subject, userID)
	if err != nil {
		return fmt.Errorf("data: link sso identity: %w", wrapConflict(err))
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateUsername updates a user's username in place -- used by P3-C's
// UpsertSSOUser when an already-linked SSO user's preferred_username/email
// changed at the IdP since their last login.
func (s *Store) UpdateUsername(ctx context.Context, userID uuid.UUID, username string) error {
	q := `UPDATE users SET username = $1 WHERE id = $2`
	tag, err := s.pool.Exec(ctx, q, username, userID)
	if err != nil {
		return fmt.Errorf("data: update username: %w", wrapConflict(err))
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetUser fetches a user by ID.
func (s *Store) GetUser(ctx context.Context, id uuid.UUID) (*User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE id = $1`
	row := s.pool.QueryRow(ctx, q, id)
	out := &User{}
	if err := scanUser(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get user: %w", err)
	}
	return out, nil
}

// GetUserByUsername fetches a user by username — the primary lookup used
// by the login flow (WS-F).
func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	q := `SELECT ` + userColumns + ` FROM users WHERE username = $1`
	row := s.pool.QueryRow(ctx, q, username)
	out := &User{}
	if err := scanUser(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: get user by username: %w", err)
	}
	return out, nil
}

// ListUsers returns every user, ordered by username.
func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	q := `SELECT ` + userColumns + ` FROM users ORDER BY username`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("data: list users: %w", err)
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u := &User{}
		if err := scanUser(rows, u); err != nil {
			return nil, fmt.Errorf("data: list users: scan: %w", err)
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list users: %w", err)
	}
	return out, nil
}

// UpdateUser updates a user's password hash and/or is_admin flag.
func (s *Store) UpdateUser(ctx context.Context, u *User) (*User, error) {
	q := `UPDATE users SET password_hash = $1, is_admin = $2 WHERE id = $3
		RETURNING ` + userColumns
	row := s.pool.QueryRow(ctx, q, u.PasswordHash, u.IsAdmin, u.ID)
	out := &User{}
	if err := scanUser(row, out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("data: update user: %w", wrapConflict(err))
	}
	return out, nil
}

// DeleteUser hard-deletes a user (users has no soft-delete concept — only
// records do, per the locked decision).
func (s *Store) DeleteUser(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("data: delete user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
