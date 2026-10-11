package api

// handleAuthMe implements GET /api/auth/me (contracts/openapi.yaml, added
// Phase 3/P3-C): lets the frontend recover identity after a full-page
// redirect (the /auth/sso/callback flow in particular) where no in-memory
// login response is available to read the user from -- P3-D flagged that
// AuthProvider never called a "who am I" endpoint on mount; this is the
// backend half of closing that gap (see frontend/src/state/auth.ts for the
// matching frontend fix).

import (
	"net/http"
)

// meResponse is GET /api/auth/me. Customer, NeedsOnboarding and the two
// scope flags drive the frontend's routing: onboarding first, then the
// customer's resources, or the platform admin views.
type meResponse struct {
	User            userResponse      `json:"user"`
	Customer        *customerResponse `json:"customer"`
	NeedsOnboarding bool              `json:"needs_onboarding"`
	IsPlatformAdmin bool              `json:"is_platform_admin"`
	IsCustomerAdmin bool              `json:"is_customer_admin"`
}

// handleAuthMe returns the authenticated caller's current user, re-derived
// from the DB rather than only the session cookie's claims, so a role or
// customer change since the session was issued (e.g. completing
// onboarding) is reflected immediately rather than only after the session
// is reissued.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	t, err := s.loadTenant(r.Context(), r)
	if err != nil {
		writeErr(w, err)
		return
	}
	authProvider := t.user.AuthProvider
	if authProvider == "" {
		authProvider = "local"
	}
	out := meResponse{
		User: userResponse{
			ID:           t.user.ID,
			Username:     t.user.Username,
			AuthProvider: authProvider,
			Roles:        t.roles,
			IsAdmin:      t.platformAdmin,
		},
		NeedsOnboarding: t.needsOnboarding(),
		IsPlatformAdmin: t.platformAdmin,
		IsCustomerAdmin: t.customerAdmin && t.customer != nil,
	}
	if t.customer != nil {
		c := toCustomerResponse(t.customer)
		out.Customer = &c
	}
	writeJSON(w, http.StatusOK, out)
}
