package data

// These tests need a real Postgres instance: internal/data IS the Postgres
// access layer (unlike e.g. internal/naming, which tests against a
// fake_store_test.go seam), so there is nothing to fake against, and no
// existing seam/pattern in this package for testing without a DB. Set
// TEST_DATABASE_URL to a connection string for a database with migrations
// 000001 and 000002 applied (see backend/db/migrations) to run them;
// otherwise every test in this package skips itself.

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testStore connects to TEST_DATABASE_URL, or skips the test if it isn't
// set.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping data package integration test (requires Postgres with migrations 000001+000002 applied)")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connect to TEST_DATABASE_URL: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool)
}

// createTestUser inserts a throwaway user with a unique username and
// registers cleanup to delete it (which cascades to user_roles /
// audit_log.actor_user_id via the FKs added in migration 000002).
func createTestUser(t *testing.T, s *Store, usernamePrefix string) *User {
	t.Helper()
	ctx := context.Background()
	u, err := s.CreateUser(ctx, &User{
		Username:     usernamePrefix + "-" + uuid.NewString(),
		PasswordHash: "test-hash",
		IsAdmin:      false,
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		_ = s.DeleteUser(context.Background(), u.ID)
	})
	return u
}

func TestRoles_AssignHasRevoke(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "roles-test")

	// P3-C fix: CreateUser now grants "requester" immediately on creation
	// (previously a freshly created user had no roles at all until
	// something separately called AssignRole(RoleRequester) -- see
	// users.go's CreateUser doc comment for the gap this closed).
	roles, err := s.GetUserRoles(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
	if len(roles) != 1 || roles[0] != RoleRequester {
		t.Fatalf("expected a freshly created user to have [requester], got %v", roles)
	}

	// Re-assigning requester here is deliberately redundant (AssignRole is
	// idempotent) -- kept so this test still explicitly documents that
	// re-assigning an already-held role is a safe no-op.
	if err := s.AssignRole(ctx, u.ID, RoleRequester); err != nil {
		t.Fatalf("AssignRole(requester): %v", err)
	}
	if err := s.AssignRole(ctx, u.ID, RoleAdmin); err != nil {
		t.Fatalf("AssignRole(admin): %v", err)
	}
	// Idempotent re-assign.
	if err := s.AssignRole(ctx, u.ID, RoleAdmin); err != nil {
		t.Fatalf("AssignRole(admin) (idempotent): %v", err)
	}

	has, err := s.HasRole(ctx, u.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("HasRole(admin): %v", err)
	}
	if !has {
		t.Fatal("expected HasRole(admin) = true")
	}

	roles, err = s.GetUserRoles(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
	if len(roles) != 2 {
		t.Fatalf("expected 2 roles (admin, requester), got %v", roles)
	}

	if err := s.RevokeRole(ctx, u.ID, RoleAdmin); err != nil {
		t.Fatalf("RevokeRole(admin): %v", err)
	}
	// Idempotent re-revoke.
	if err := s.RevokeRole(ctx, u.ID, RoleAdmin); err != nil {
		t.Fatalf("RevokeRole(admin) (idempotent): %v", err)
	}
	// Revoking a role never held is also a no-op.
	if err := s.RevokeRole(ctx, u.ID, RoleApprover); err != nil {
		t.Fatalf("RevokeRole(approver) (never held): %v", err)
	}

	has, err = s.HasRole(ctx, u.ID, RoleAdmin)
	if err != nil {
		t.Fatalf("HasRole(admin) after revoke: %v", err)
	}
	if has {
		t.Fatal("expected HasRole(admin) = false after revoke")
	}

	roles, err = s.GetUserRoles(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserRoles after revoke: %v", err)
	}
	if len(roles) != 1 || roles[0] != RoleRequester {
		t.Fatalf("expected [requester] after revoking admin, got %v", roles)
	}
}

func TestRoles_AssignUnknownRole(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	u := createTestUser(t, s, "roles-test-unknown")

	err := s.AssignRole(ctx, u.ID, "not-a-real-role")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for an unknown role name, got %v", err)
	}
}
