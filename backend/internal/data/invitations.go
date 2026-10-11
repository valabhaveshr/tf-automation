package data

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrInvitationInvalid is returned when an invitation token doesn't exist,
// was already accepted, was revoked, or has expired. Deliberately one error
// for all four, so the public accept endpoint doesn't reveal which.
var ErrInvitationInvalid = errors.New("data: invitation is invalid, expired, or already used")

// ErrAlreadyOnboarded is returned by CompleteOnboarding when the user
// already belongs to a customer.
var ErrAlreadyOnboarded = errors.New("data: user already belongs to a customer")

const invitationColumns = `id, token_hash, email, role, customer_id, invited_by, created_at, expires_at, accepted_at, accepted_user_id, revoked_at`

func scanInvitation(row rowScanner, i *Invitation) error {
	return row.Scan(&i.ID, &i.TokenHash, &i.Email, &i.Role, &i.CustomerID, &i.InvitedBy,
		&i.CreatedAt, &i.ExpiresAt, &i.AcceptedAt, &i.AcceptedUserID, &i.RevokedAt)
}

// CreateInvitation inserts a new invitation. inv.TokenHash, Email, Role,
// InvitedBy and ExpiresAt must be set; CustomerID nil means "new customer".
func (s *Store) CreateInvitation(ctx context.Context, inv *Invitation) (*Invitation, error) {
	q := `INSERT INTO invitations (token_hash, email, role, customer_id, invited_by, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + invitationColumns
	row := s.pool.QueryRow(ctx, q, inv.TokenHash, inv.Email, inv.Role, inv.CustomerID, inv.InvitedBy, inv.ExpiresAt)
	out := &Invitation{}
	if err := scanInvitation(row, out); err != nil {
		return nil, fmt.Errorf("data: create invitation: %w", wrapConflict(err))
	}
	return out, nil
}

// GetUsableInvitation returns the invitation for tokenHash if it can still
// be accepted, else ErrInvitationInvalid.
func (s *Store) GetUsableInvitation(ctx context.Context, tokenHash string) (*Invitation, error) {
	q := `SELECT ` + invitationColumns + ` FROM invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()`
	out := &Invitation{}
	if err := scanInvitation(s.pool.QueryRow(ctx, q, tokenHash), out); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvitationInvalid
		}
		return nil, fmt.Errorf("data: get invitation: %w", err)
	}
	return out, nil
}

// ListInvitations returns invitations newest first. customerID nil lists
// every invitation (platform admin view); otherwise only that customer's.
func (s *Store) ListInvitations(ctx context.Context, customerID *uuid.UUID) ([]*Invitation, error) {
	q := `SELECT ` + invitationColumns + ` FROM invitations`
	var args []any
	if customerID != nil {
		q += ` WHERE customer_id = $1`
		args = append(args, *customerID)
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("data: list invitations: %w", err)
	}
	defer rows.Close()

	var out []*Invitation
	for rows.Next() {
		i := &Invitation{}
		if err := scanInvitation(rows, i); err != nil {
			return nil, fmt.Errorf("data: list invitations: scan: %w", err)
		}
		out = append(out, i)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("data: list invitations: %w", err)
	}
	return out, nil
}

// RevokeInvitation revokes a pending invitation. customerID non-nil scopes
// the revoke to that customer's invitations (customer admins), so one
// customer can't revoke another's. Returns ErrNotFound if nothing pending
// matched.
func (s *Store) RevokeInvitation(ctx context.Context, id uuid.UUID, customerID *uuid.UUID) error {
	q := `UPDATE invitations SET revoked_at = now()
		WHERE id = $1 AND accepted_at IS NULL AND revoked_at IS NULL`
	args := []any{id}
	if customerID != nil {
		q += ` AND customer_id = $2`
		args = append(args, *customerID)
	}
	tag, err := s.pool.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("data: revoke invitation: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AcceptInvitation atomically claims the invitation for tokenHash and
// creates the invited user (customer_id and role taken from the
// invitation, plus the baseline requester role). A taken username returns a
// *ConflictError and leaves the invitation usable.
func (s *Store) AcceptInvitation(ctx context.Context, tokenHash, username, passwordHash string) (*User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("data: accept invitation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	inv := &Invitation{}
	q := `SELECT ` + invitationColumns + ` FROM invitations
		WHERE token_hash = $1 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()
		FOR UPDATE`
	if err := scanInvitation(tx.QueryRow(ctx, q, tokenHash), inv); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvitationInvalid
		}
		return nil, fmt.Errorf("data: accept invitation: %w", err)
	}

	u := &User{}
	insertUser := `INSERT INTO users (username, password_hash, is_admin, auth_provider, customer_id, email)
		VALUES ($1, $2, false, 'local', $3, $4)
		RETURNING ` + userColumns
	if err := scanUser(tx.QueryRow(ctx, insertUser, username, passwordHash, inv.CustomerID, inv.Email), u); err != nil {
		return nil, fmt.Errorf("data: accept invitation: create user: %w", wrapConflict(err))
	}

	roles := []string{RoleRequester}
	if inv.Role != RoleRequester {
		roles = append(roles, inv.Role)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role_id)
		SELECT $1, id FROM roles WHERE name = ANY($2) ON CONFLICT DO NOTHING`, u.ID, roles); err != nil {
		return nil, fmt.Errorf("data: accept invitation: assign roles: %w", err)
	}

	if _, err := tx.Exec(ctx, `UPDATE invitations SET accepted_at = $1, accepted_user_id = $2 WHERE id = $3`,
		time.Now(), u.ID, inv.ID); err != nil {
		return nil, fmt.Errorf("data: accept invitation: mark accepted: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("data: accept invitation: commit: %w", err)
	}
	u.Roles = roles
	return u, nil
}

// CompleteOnboarding creates the customer described by c and makes it the
// tenant of userID, in one transaction. A duplicate org name or short code
// returns a *ConflictError; a user who already has a customer returns
// ErrAlreadyOnboarded. Neither leaves a customer row behind.
func (s *Store) CompleteOnboarding(ctx context.Context, userID uuid.UUID, c *Customer) (*Customer, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("data: complete onboarding: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	out := &Customer{}
	if err := scanCustomer(tx.QueryRow(ctx, insertCustomerSQL, customerInsertArgs(c)...), out); err != nil {
		return nil, fmt.Errorf("data: complete onboarding: create customer: %w", wrapConflict(err))
	}

	tag, err := tx.Exec(ctx, `UPDATE users SET customer_id = $1 WHERE id = $2 AND customer_id IS NULL`, out.ID, userID)
	if err != nil {
		return nil, fmt.Errorf("data: complete onboarding: link user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAlreadyOnboarded
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("data: complete onboarding: commit: %w", err)
	}
	return out, nil
}
