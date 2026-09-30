package api

// This file (P3-C) implements auth.UserUpserter (P3-B's small decoupling
// interface, see internal/auth/oidc.go) backed by WS-A/P3-A's data.Store,
// turning a verified OIDC identity into a local user record + role set.
// Mirrors auth_adapter.go's userLookupAdapter pattern: WS-F/P3-B
// deliberately don't depend on the concrete data package, so this
// workstream (P3-C) supplies the real implementation at integration time.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/data"
)

// ssoUserUpserter implements auth.UserUpserter backed by data.Store.
type ssoUserUpserter struct {
	store *data.Store
}

// NewSSOUserUpserter constructs the auth.UserUpserter implementation
// cmd/server/main.go wires into auth.OIDCProvider.Users once an OIDC
// provider is configured (see auth.NewOIDCProvider's nil-when-unconfigured
// contract).
func NewSSOUserUpserter(store *data.Store) auth.UserUpserter {
	return &ssoUserUpserter{store: store}
}

var _ auth.UserUpserter = (*ssoUserUpserter)(nil)

// errSSOUsernameCollision is the error surfaced when the IdP-supplied
// username belongs to a local account already linked to a *different*
// sso_subject -- see UpsertSSOUser's doc comment for the reasoning.
// auth.OIDCProvider.HandleCallback treats any non-nil error from
// UpsertSSOUser identically (a generic 401), so this never leaks past
// this package, but it's exported as a distinct value for this package's
// own tests/logging to recognize.
var errSSOUsernameCollision = errors.New("api: sso username already linked to a different identity")

// UpsertSSOUser implements auth.UserUpserter.
//
// Judgment call, documented per the task brief (username-collision-with-a-
// different-subject: link or reject?): subject (the IdP's stable 'sub'
// claim) is the durable identity key, never username, since usernames can
// be renamed or reused at the IdP. On each callback:
//
//  1. Look up by sso_subject. Found -> that IS the user; update the stored
//     username in place if it drifted at the IdP (e.g. renamed), and
//     return their current roles.
//  2. Not found by subject -> look up by username.
//     a. No local account with that username -> create a brand-new one
//        (auth_provider="oidc", sso_subject=subject, a random/unusable
//        password_hash since SSO users never authenticate with a
//        password). data.Store.CreateUser assigns the baseline
//        "requester" role as part of creation (P3-A's gap fix).
//     b. A local account with that username exists and has NO sso_subject
//        yet (a Phase-1-era built-in account, auth_provider="local") ->
//        link it in place: set sso_subject/auth_provider on the existing
//        row rather than creating a duplicate user. This is the intended
//        built-in-to-SSO migration path called for by
//        plan/ARCHITECTURE.md's Phase 3 risk table ("map built-in
//        accounts to IdP identities rather than a hard cutover") --
//        it preserves the existing user's id, roles, and history.
//     c. A local account with that username exists and already has a
//        DIFFERENT sso_subject -> REJECT (errSSOUsernameCollision) rather
//        than silently re-linking. Silently reassigning would let one IdP
//        identity (e.g. a reused/renamed username at the IdP, or two
//        different people who happened to share a username at different
//        points in time) take over an account it doesn't actually own.
//        A genuine "this username moved to a new IdP identity" case is
//        rare enough, and security-sensitive enough, that it should be a
//        deliberate admin action (e.g. RevokeRole/manual DB fix), not an
//        automatic login-time side effect.
func (a *ssoUserUpserter) UpsertSSOUser(ctx context.Context, subject, username string) (uuid.UUID, []string, error) {
	if bySubject, err := a.store.GetUserBySSOSubject(ctx, subject); err == nil {
		if bySubject.Username != username {
			if err := a.store.UpdateUsername(ctx, bySubject.ID, username); err != nil {
				return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: update username: %w", err)
			}
		}
		roles, err := a.store.GetUserRoles(ctx, bySubject.ID)
		if err != nil {
			return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: get roles: %w", err)
		}
		return bySubject.ID, roles, nil
	} else if !errors.Is(err, data.ErrNotFound) {
		return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: lookup by subject: %w", err)
	}

	existing, err := a.store.GetUserByUsername(ctx, username)
	if err == nil {
		if existing.SSOSubject != nil && *existing.SSOSubject != subject {
			return uuid.Nil, nil, errSSOUsernameCollision
		}
		if err := a.store.LinkSSOIdentity(ctx, existing.ID, subject); err != nil {
			return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: link identity: %w", err)
		}
		roles, err := a.store.GetUserRoles(ctx, existing.ID)
		if err != nil {
			return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: get roles: %w", err)
		}
		return existing.ID, roles, nil
	} else if !errors.Is(err, data.ErrNotFound) {
		return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: lookup by username: %w", err)
	}

	hash, err := randomUnusablePasswordHash()
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: generate password hash: %w", err)
	}
	subjectCopy := subject
	created, err := a.store.CreateUser(ctx, &data.User{
		Username:     username,
		PasswordHash: hash,
		AuthProvider: "oidc",
		SSOSubject:   &subjectCopy,
	})
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: create: %w", err)
	}
	roles, err := a.store.GetUserRoles(ctx, created.ID)
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("api: upsert sso user: get roles: %w", err)
	}
	return created.ID, roles, nil
}

// randomUnusablePasswordHash returns a bcrypt hash of 32 cryptographically
// random bytes -- a valid password_hash value that no plaintext password
// will ever match (VerifyPassword can never succeed against it), for
// SSO-provisioned users who never authenticate with a password.
func randomUnusablePasswordHash() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return auth.HashPassword(base64.RawURLEncoding.EncodeToString(buf))
}
