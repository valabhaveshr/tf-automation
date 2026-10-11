package data

// Postgres-backed like roles_test.go: needs TEST_DATABASE_URL with
// migrations 000001-000003 applied, else every test here skips.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func uniqueShortCode() string {
	// 5 chars, lowercase alphanumeric: matches naming's short-code rule.
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:5]
}

func deleteCustomerCascade(t *testing.T, s *Store, customerID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	_, _ = s.pool.Exec(ctx, `DELETE FROM invitations WHERE customer_id = $1`, customerID)
	_, _ = s.pool.Exec(ctx, `UPDATE users SET customer_id = NULL WHERE customer_id = $1`, customerID)
	_ = s.DeleteCustomer(ctx, customerID)
}

func TestTenancy_InviteAcceptOnboard(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Platform admin invites a customer admin for a new customer.
	tokenHash := "test-" + uuid.NewString()
	inv, err := s.CreateInvitation(ctx, &Invitation{
		TokenHash: tokenHash, Email: "owner@example.com", Role: RoleCustomerAdmin,
		InvitedBy: "platform-admin", ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	t.Cleanup(func() { _, _ = s.pool.Exec(context.Background(), `DELETE FROM invitations WHERE id = $1`, inv.ID) })

	if _, err := s.GetUsableInvitation(ctx, tokenHash); err != nil {
		t.Fatalf("GetUsableInvitation: %v", err)
	}

	u, err := s.AcceptInvitation(ctx, tokenHash, "owner-"+uuid.NewString(), "hash")
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	t.Cleanup(func() { _ = s.DeleteUser(context.Background(), u.ID) })
	if u.CustomerID != nil {
		t.Errorf("new-customer invitee should have no customer yet, got %v", *u.CustomerID)
	}
	roles, err := s.GetUserRoles(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserRoles: %v", err)
	}
	if strings.Join(roles, ",") != "customer_admin,requester" {
		t.Errorf("roles = %v, want [customer_admin requester]", roles)
	}

	// The token is single-use.
	if _, err := s.AcceptInvitation(ctx, tokenHash, "second-"+uuid.NewString(), "hash"); !errors.Is(err, ErrInvitationInvalid) {
		t.Errorf("second accept: got %v, want ErrInvitationInvalid", err)
	}

	// Onboarding creates the customer and links the user.
	code := uniqueShortCode()
	cust, err := s.CompleteOnboarding(ctx, u.ID, &Customer{
		Name: "Doha Marine Sports Centre " + code, BusinessShortCode: code,
		ContactEmail: "it@example.com", DefaultRegion: "me-central1",
	})
	if err != nil {
		t.Fatalf("CompleteOnboarding: %v", err)
	}
	t.Cleanup(func() { deleteCustomerCascade(t, s, cust.ID) })
	if cust.DefaultCloud != "gcp" || cust.DefaultRegion != "me-central1" || cust.ContactEmail != "it@example.com" {
		t.Errorf("profile not stored as expected: %+v", cust)
	}
	got, err := s.GetUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.CustomerID == nil || *got.CustomerID != cust.ID {
		t.Fatalf("user not linked to the new customer: %+v", got.CustomerID)
	}

	// A second onboarding by the same user is refused.
	if _, err := s.CompleteOnboarding(ctx, u.ID, &Customer{Name: "Other " + code, BusinessShortCode: uniqueShortCode()}); !errors.Is(err, ErrAlreadyOnboarded) {
		t.Errorf("second onboarding: got %v, want ErrAlreadyOnboarded", err)
	}

	team, err := s.ListUsersByCustomer(ctx, cust.ID)
	if err != nil {
		t.Fatalf("ListUsersByCustomer: %v", err)
	}
	if len(team) != 1 || team[0].ID != u.ID || len(team[0].Roles) != 2 {
		t.Errorf("team = %+v, want just the onboarded admin with 2 roles", team)
	}
}

func TestTenancy_DuplicateShortCodeRejected(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	code := uniqueShortCode()

	first := createTestUser(t, s, "dup-a")
	cust, err := s.CompleteOnboarding(ctx, first.ID, &Customer{Name: "First " + code, BusinessShortCode: code})
	if err != nil {
		t.Fatalf("first onboarding: %v", err)
	}
	t.Cleanup(func() { deleteCustomerCascade(t, s, cust.ID) })

	// Same short code in a different case still collides.
	second := createTestUser(t, s, "dup-b")
	_, err = s.CompleteOnboarding(ctx, second.ID, &Customer{Name: "Second " + code, BusinessShortCode: strings.ToUpper(code)})
	var conflict *ConflictError
	if !errors.As(err, &conflict) || conflict.Constraint != "customers_business_short_code_key" {
		t.Fatalf("got %v, want a conflict on customers_business_short_code_key", err)
	}
	// The failed onboarding left the user unlinked.
	got, _ := s.GetUser(ctx, second.ID)
	if got.CustomerID != nil {
		t.Errorf("failed onboarding linked the user to %v", *got.CustomerID)
	}
}

func TestTenancy_RevokeIsScopedToCustomer(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	owner := createTestUser(t, s, "scope")
	cust, err := s.CompleteOnboarding(ctx, owner.ID, &Customer{Name: "Scope " + uuid.NewString(), BusinessShortCode: uniqueShortCode()})
	if err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	t.Cleanup(func() { deleteCustomerCascade(t, s, cust.ID) })

	inv, err := s.CreateInvitation(ctx, &Invitation{
		TokenHash: "test-" + uuid.NewString(), Email: "dev@example.com", Role: RoleRequester,
		CustomerID: &cust.ID, InvitedBy: owner.Username, ExpiresAt: time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	other := uuid.New()
	if err := s.RevokeInvitation(ctx, inv.ID, &other); !errors.Is(err, ErrNotFound) {
		t.Errorf("revoke from another customer: got %v, want ErrNotFound", err)
	}
	if err := s.RevokeInvitation(ctx, inv.ID, &cust.ID); err != nil {
		t.Errorf("revoke from own customer: %v", err)
	}
	if _, err := s.GetUsableInvitation(ctx, inv.TokenHash); !errors.Is(err, ErrInvitationInvalid) {
		t.Errorf("revoked invitation still usable: %v", err)
	}
}
