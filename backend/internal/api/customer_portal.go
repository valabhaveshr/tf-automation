package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/naming"
)

// Customer-facing tenancy endpoints (migration 000003): onboarding, the
// caller's customer profile, team members, and invitations. See tenancy.go
// for the platform-admin / customer-user model.

const invitationTTL = 7 * 24 * time.Hour

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._@-]{3,64}$`)

const minPasswordLength = 8

// customerProfileRequest is the onboarding form, and the profile edit form
// (where BusinessShortCode is ignored for customer admins).
type customerProfileRequest struct {
	Name              string `json:"name"`
	BusinessShortCode string `json:"business_short_code"`
	ContactName       string `json:"contact_name"`
	ContactEmail      string `json:"contact_email"`
	ContactPhone      string `json:"contact_phone"`
	Country           string `json:"country"`
	Industry          string `json:"industry"`
	DefaultCloud      string `json:"default_cloud"`
	DefaultRegion     string `json:"default_region"`
	GCPOrganizationID string `json:"gcp_organization_id"`
	BillingAccountID  string `json:"billing_account_id"`
}

// toCustomer validates the request and maps it onto c (a new or existing
// customer). The short code is only applied when setShortCode is true.
func (req customerProfileRequest) toCustomer(c *data.Customer, setShortCode bool) error {
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > 200 {
		return badRequest("organization name is required (max 200 characters)")
	}
	c.Name = name
	if setShortCode {
		code, err := naming.NormalizeBusinessShortCode(req.BusinessShortCode)
		if err != nil {
			return badRequest(err.Error())
		}
		c.BusinessShortCode = code
	}
	c.ContactName = strings.TrimSpace(req.ContactName)
	c.ContactEmail = strings.TrimSpace(req.ContactEmail)
	if c.ContactEmail != "" {
		if _, err := mail.ParseAddress(c.ContactEmail); err != nil {
			return badRequest("contact email is not a valid email address")
		}
	}
	c.ContactPhone = strings.TrimSpace(req.ContactPhone)
	c.Country = strings.TrimSpace(req.Country)
	c.Industry = strings.TrimSpace(req.Industry)
	c.DefaultCloud = strings.ToLower(strings.TrimSpace(req.DefaultCloud))
	switch c.DefaultCloud {
	case "", "gcp", "aws", "azure":
	default:
		return badRequest("default cloud must be gcp, aws or azure")
	}
	c.DefaultRegion = strings.TrimSpace(req.DefaultRegion)
	c.GCPOrganizationID = strings.TrimSpace(req.GCPOrganizationID)
	c.BillingAccountID = strings.TrimSpace(req.BillingAccountID)
	return nil
}

// handleOnboarding implements POST /api/onboarding: a customer admin with no
// customer yet creates their customer and becomes its first member.
func (s *Server) handleOnboarding(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if !t.needsOnboarding() {
		writeErr(w, conflictErr("onboarding is already complete for this account"))
		return
	}

	var req customerProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	c := &data.Customer{}
	if err := req.toCustomer(c, true); err != nil {
		writeErr(w, err)
		return
	}

	created, err := s.Store.CompleteOnboarding(ctx, t.user.ID, c)
	if err != nil {
		if errors.Is(err, data.ErrAlreadyOnboarded) {
			writeErr(w, conflictErr("onboarding is already complete for this account"))
			return
		}
		writeErr(w, err)
		return
	}
	s.logAudit(r, "customer.onboard", "customer", strPtr(created.ID.String()), map[string]string{
		"name":                created.Name,
		"business_short_code": created.BusinessShortCode,
	})
	writeJSON(w, http.StatusCreated, toCustomerResponse(created))
}

// handleGetMyCustomer implements GET /api/customer.
func (s *Server) handleGetMyCustomer(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTenant(r.Context(), r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if t.customer == nil {
		writeErr(w, errNoCustomer(t))
		return
	}
	writeJSON(w, http.StatusOK, toCustomerResponse(t.customer))
}

// handleUpdateMyCustomer implements PUT /api/customer (customer admins).
// The short name is fixed after onboarding: it's baked into generated
// resource names and naming counters. A platform admin can still change it
// via the admin customer endpoint.
func (s *Server) handleUpdateMyCustomer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if t.customer == nil {
		writeErr(w, errNoCustomer(t))
		return
	}
	if !t.customerAdmin {
		writeErr(w, forbidden("only a customer admin can edit the organization profile"))
		return
	}
	var req customerProfileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	c := *t.customer
	if err := req.toCustomer(&c, false); err != nil {
		writeErr(w, err)
		return
	}
	updated, err := s.Store.UpdateCustomer(ctx, &c)
	if err != nil {
		writeErr(w, err)
		return
	}
	s.logAudit(r, "customer.update", "customer", strPtr(updated.ID.String()), nil)
	writeJSON(w, http.StatusOK, toCustomerResponse(updated))
}

type teamMemberResponse struct {
	ID        uuid.UUID `json:"id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Roles     []string  `json:"roles"`
	CreatedAt time.Time `json:"created_at"`
}

// handleListTeam implements GET /api/customer/users: the members of the
// caller's customer. Any member may see who else is on their team.
func (s *Server) handleListTeam(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if t.customer == nil {
		writeErr(w, errNoCustomer(t))
		return
	}
	s.writeTeam(w, r, t.customer.ID)
}

// handleAdminListCustomerUsers implements GET /api/admin/customers/{id}/users.
func (s *Server) handleAdminListCustomerUsers(w http.ResponseWriter, r *http.Request) {
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid customer id"))
		return
	}
	s.writeTeam(w, r, id)
}

func (s *Server) writeTeam(w http.ResponseWriter, r *http.Request, customerID uuid.UUID) {
	users, err := s.Store.ListUsersByCustomer(r.Context(), customerID)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]teamMemberResponse, 0, len(users))
	for _, u := range users {
		out = append(out, teamMemberResponse{ID: u.ID, Username: u.Username, Email: u.Email, Roles: u.Roles, CreatedAt: u.CreatedAt})
	}
	writeJSON(w, http.StatusOK, out)
}

// ---- Invitations ----

type createInvitationRequest struct {
	Email      string     `json:"email"`
	Role       string     `json:"role"`        // customer_admin | requester
	CustomerID *uuid.UUID `json:"customer_id"` // platform admins only; nil = new customer
}

type invitationResponse struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email"`
	Role         string     `json:"role"`
	CustomerID   *uuid.UUID `json:"customer_id,omitempty"`
	CustomerName string     `json:"customer_name,omitempty"`
	InvitedBy    string     `json:"invited_by"`
	CreatedAt    time.Time  `json:"created_at"`
	ExpiresAt    time.Time  `json:"expires_at"`
	Status       string     `json:"status"` // pending | accepted | revoked | expired
	// InvitePath is only returned when the invitation is created: the raw
	// token isn't stored, so the link can't be shown again later.
	InvitePath string `json:"invite_path,omitempty"`
}

func invitationStatus(inv *data.Invitation) string {
	switch {
	case inv.AcceptedAt != nil:
		return "accepted"
	case inv.RevokedAt != nil:
		return "revoked"
	case time.Now().After(inv.ExpiresAt):
		return "expired"
	default:
		return "pending"
	}
}

func (s *Server) toInvitationResponse(r *http.Request, inv *data.Invitation, names map[uuid.UUID]string) invitationResponse {
	out := invitationResponse{
		ID: inv.ID, Email: inv.Email, Role: inv.Role, CustomerID: inv.CustomerID,
		InvitedBy: inv.InvitedBy, CreatedAt: inv.CreatedAt, ExpiresAt: inv.ExpiresAt,
		Status: invitationStatus(inv),
	}
	if inv.CustomerID != nil {
		if name, ok := names[*inv.CustomerID]; ok {
			out.CustomerName = name
		} else if c, err := s.Store.GetCustomer(r.Context(), *inv.CustomerID); err == nil {
			names[c.ID] = c.Name
			out.CustomerName = c.Name
		}
	}
	return out
}

func newInviteToken() (raw, hash string, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashInviteToken(raw), nil
}

func hashInviteToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// handleCreateInvitation implements POST /api/invitations.
//   - Platform admin: invites a customer admin for a new customer
//     (customer_id omitted; they complete onboarding on first login), or
//     any role into an existing customer.
//   - Customer admin: invites a requester or another customer admin into
//     their own customer (customer_id ignored).
func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var req createInvitationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	email := strings.TrimSpace(req.Email)
	if _, err := mail.ParseAddress(email); err != nil {
		writeErr(w, badRequest("a valid email address is required"))
		return
	}
	role := req.Role
	if role == "" {
		role = data.RoleRequester
	}
	if role != data.RoleRequester && role != data.RoleCustomerAdmin {
		writeErr(w, badRequest("role must be requester or customer_admin"))
		return
	}

	var customerID *uuid.UUID
	switch {
	case t.platformAdmin:
		if req.CustomerID != nil {
			if _, err := s.Store.GetCustomer(ctx, *req.CustomerID); err != nil {
				if errors.Is(err, data.ErrNotFound) {
					writeErr(w, badRequest("unknown customer_id"))
					return
				}
				writeErr(w, err)
				return
			}
			customerID = req.CustomerID
		} else if role != data.RoleCustomerAdmin {
			writeErr(w, badRequest("a new customer's first user must be a customer_admin"))
			return
		}
	case t.customerAdmin && t.customer != nil:
		id := t.customer.ID
		customerID = &id
	default:
		writeErr(w, forbidden("only platform admins and customer admins can invite users"))
		return
	}

	raw, hash, err := newInviteToken()
	if err != nil {
		writeErr(w, err)
		return
	}
	inv, err := s.Store.CreateInvitation(ctx, &data.Invitation{
		TokenHash:  hash,
		Email:      email,
		Role:       role,
		CustomerID: customerID,
		InvitedBy:  t.user.Username,
		ExpiresAt:  time.Now().Add(invitationTTL),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	s.logAudit(r, "invitation.create", "invitation", strPtr(inv.ID.String()), map[string]string{
		"email": email,
		"role":  role,
	})
	out := s.toInvitationResponse(r, inv, map[uuid.UUID]string{})
	out.InvitePath = "/invite/" + raw
	writeJSON(w, http.StatusCreated, out)
}

// handleListInvitations implements GET /api/invitations: every invitation
// for a platform admin, the caller's customer's for a customer admin.
func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var scope *uuid.UUID
	switch {
	case t.platformAdmin:
	case t.customerAdmin && t.customer != nil:
		id := t.customer.ID
		scope = &id
	default:
		writeErr(w, forbidden("only platform admins and customer admins can view invitations"))
		return
	}
	invs, err := s.Store.ListInvitations(ctx, scope)
	if err != nil {
		writeErr(w, err)
		return
	}
	names := map[uuid.UUID]string{}
	out := make([]invitationResponse, 0, len(invs))
	for _, inv := range invs {
		out = append(out, s.toInvitationResponse(r, inv, names))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevokeInvitation implements DELETE /api/invitations/{id}.
func (s *Server) handleRevokeInvitation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, err := s.loadTenant(ctx, r)
	if err != nil {
		writeErr(w, err)
		return
	}
	id, err := parseUUIDParam(r, "id")
	if err != nil {
		writeErr(w, badRequest("invalid invitation id"))
		return
	}
	var scope *uuid.UUID
	switch {
	case t.platformAdmin:
	case t.customerAdmin && t.customer != nil:
		cid := t.customer.ID
		scope = &cid
	default:
		writeErr(w, forbidden("only platform admins and customer admins can revoke invitations"))
		return
	}
	if err := s.Store.RevokeInvitation(ctx, id, scope); err != nil {
		writeErr(w, err)
		return
	}
	s.logAudit(r, "invitation.revoke", "invitation", strPtr(id.String()), nil)
	w.WriteHeader(http.StatusNoContent)
}

type invitationPreviewResponse struct {
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	CustomerName string    `json:"customer_name,omitempty"` // empty = new customer (onboarding follows)
	ExpiresAt    time.Time `json:"expires_at"`
}

// handlePreviewInvitation implements GET /api/invite/{token} (public): what
// the accept page shows before the invitee picks a username and password.
func (s *Server) handlePreviewInvitation(w http.ResponseWriter, r *http.Request) {
	inv, err := s.Store.GetUsableInvitation(r.Context(), hashInviteToken(chi.URLParam(r, "token")))
	if err != nil {
		if errors.Is(err, data.ErrInvitationInvalid) {
			writeErr(w, notFound("this invitation link is invalid, expired, or has already been used"))
			return
		}
		writeErr(w, err)
		return
	}
	out := invitationPreviewResponse{Email: inv.Email, Role: inv.Role, ExpiresAt: inv.ExpiresAt}
	if inv.CustomerID != nil {
		if c, err := s.Store.GetCustomer(r.Context(), *inv.CustomerID); err == nil {
			out.CustomerName = c.Name
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type acceptInvitationRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleAcceptInvitation implements POST /api/invite/{token}/accept
// (public): creates the account and signs the new user in.
func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	var req acceptInvitationRequest
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, badRequest("invalid request body"))
		return
	}
	username := strings.TrimSpace(req.Username)
	if !usernamePattern.MatchString(username) {
		writeErr(w, badRequest("username must be 3-64 characters: letters, digits, '.', '_', '@' or '-'"))
		return
	}
	if len(req.Password) < minPasswordLength {
		writeErr(w, badRequest("password must be at least 8 characters"))
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	u, err := s.Store.AcceptInvitation(r.Context(), hashInviteToken(chi.URLParam(r, "token")), username, hash)
	if err != nil {
		if errors.Is(err, data.ErrInvitationInvalid) {
			writeErr(w, notFound("this invitation link is invalid, expired, or has already been used"))
			return
		}
		writeErr(w, err)
		return
	}
	if err := s.Sessions.IssueSessionCookie(w, u.ID, u.Username, u.Roles); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		User userResponse `json:"user"`
	}{User: userResponse{ID: u.ID, Username: u.Username, AuthProvider: "local", Roles: u.Roles}})
}
