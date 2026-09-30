package auth

import (
	"context"
	"net/http"
)

type contextKey int

const claimsContextKey contextKey = iota

// RequireAuth returns middleware that parses and validates the session
// cookie, rejecting the request with 401 if it is missing, malformed,
// tampered with, or expired. On success, the parsed *Claims are injected
// into the request context (retrievable via UserFromContext) before calling
// next.
func (s *Sessions) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, err := s.FromRequest(r)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin returns middleware that rejects the request with 403 unless
// the authenticated caller's is_admin claim is true. It must be chained
// after RequireAuth (or anything else that has already populated Claims
// into the request context), since it reads claims from context rather than
// parsing the cookie itself.
//
// This is the enforcement point for Phase 1's locked "lightweight is_admin
// flag" decision: it gates the admin-only manual catalog refresh
// (POST /catalog/refresh) and the naming/customer admin endpoints
// (/admin/customers, /admin/naming-templates) -- NOT full RBAC, which is
// Phase 3's job. Phase 3 is expected to replace this is_admin check with a
// real role check at this same enforcement point.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := UserFromContext(r.Context())
		if !ok || !claims.IsAdmin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireRole returns middleware that rejects the request with 403 unless
// the authenticated caller's Roles claim contains roleName. Must be chained
// after RequireAuth, same as RequireAdmin. Unlike RequireAdmin (which is
// pinned to the "admin" role specifically for backward compatibility with
// Phase 1's is_admin-only enforcement points), this is parameterized so a
// future endpoint can gate on any role -- e.g. "approver" once Phase 2
// resumes and has something for that role to gate. Nothing calls this yet
// (P3-C wires it in for future use per the task brief), so it is exported
// but currently unreferenced within this module.
func RequireRole(roleName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := UserFromContext(r.Context())
			if !ok || !containsRole(claims.Roles, roleName) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// UserFromContext retrieves the *Claims injected into the request context
// by RequireAuth. The bool return reports whether claims were present.
func UserFromContext(ctx context.Context) (*Claims, bool) {
	claims, ok := ctx.Value(claimsContextKey).(*Claims)
	return claims, ok
}
