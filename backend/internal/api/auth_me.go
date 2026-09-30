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

	"tf-automation/backend/internal/auth"
)

// handleAuthMe returns the authenticated caller's current user, re-derived
// from the DB rather than only the session cookie's claims, so a role
// change since the session was issued (e.g. an admin granting/revoking a
// role) is reflected immediately rather than only after the session is
// reissued.
func (s *Server) handleAuthMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	u, err := s.Store.GetUser(r.Context(), claims.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	roles, err := s.Store.GetUserRoles(r.Context(), claims.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}

	isAdmin := u.IsAdmin
	for _, role := range roles {
		if role == "admin" {
			isAdmin = true
			break
		}
	}

	authProvider := u.AuthProvider
	if authProvider == "" {
		authProvider = "local"
	}

	writeJSON(w, http.StatusOK, struct {
		User userResponse `json:"user"`
	}{
		User: userResponse{
			ID:           u.ID,
			Username:     u.Username,
			AuthProvider: authProvider,
			Roles:        roles,
			IsAdmin:      isAdmin,
		},
	})
}
