package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"

	"github.com/google/uuid"
)

// UserLookup is the minimal interface LoginHandler needs to authenticate a
// user. It's defined here, not imported from WS-A's concrete data package,
// so this workstream stays decoupled and independently testable per Phase
// 0's parallel-workstream contract; WS-G wires a real implementation
// (backed by internal/data) at consolidation time.
type UserLookup interface {
	// GetByUsername looks up a user by username. Implementations should
	// return ErrUserNotFound (or any non-nil error) when no such user
	// exists; LoginHandler always responds with a generic 401 either way,
	// never revealing whether the username exists.
	//
	// BREAKING CHANGE (P3-C): roles []string was added alongside the
	// existing return values so LoginHandler can pass the caller's full
	// role set to IssueSessionCookie (which itself moved from a trailing
	// `isAdmin bool` to `roles []string` -- see session.go). isAdmin is
	// retained unchanged for the login response's is_admin field.
	GetByUsername(ctx context.Context, username string) (id uuid.UUID, passwordHash string, isAdmin bool, roles []string, err error)
}

// ErrUserNotFound is a sentinel a UserLookup implementation may return when
// no user matches the given username.
var ErrUserNotFound = errors.New("auth: user not found")

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// userResponse mirrors the OpenAPI contract's #/components/schemas/User
// shape: {id, username, auth_provider, roles, is_admin}.
type userResponse struct {
	ID           uuid.UUID `json:"id"`
	Username     string    `json:"username"`
	AuthProvider string    `json:"auth_provider"`
	Roles        []string  `json:"roles"`
	IsAdmin      bool      `json:"is_admin"`
}

// LoginHandler implements POST /api/auth/login per contracts/openapi.yaml:
// rate-limit check -> lookup user -> verify password -> issue session
// cookie -> respond {user: {id, username, is_admin}}.
type LoginHandler struct {
	Sessions    *Sessions
	Users       UserLookup
	RateLimiter *RateLimiter
}

// NewLoginHandler constructs a LoginHandler. If limiter is nil,
// DefaultLoginRateLimiter() is used.
func NewLoginHandler(sessions *Sessions, users UserLookup, limiter *RateLimiter) *LoginHandler {
	if limiter == nil {
		limiter = DefaultLoginRateLimiter()
	}
	return &LoginHandler{Sessions: sessions, Users: users, RateLimiter: limiter}
}

func (h *LoginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Rate-limit by username when present (the meaningful key for
	// credential-stuffing/brute-force protection); fall back to remote IP
	// so a blank/garbage username can't dodge the limit entirely.
	rateLimitKey := req.Username
	if rateLimitKey == "" {
		rateLimitKey = remoteIP(r)
	}
	if !h.RateLimiter.Allow(rateLimitKey) {
		http.Error(w, ErrTooManyAttempts.Error(), http.StatusTooManyRequests)
		return
	}

	if req.Username == "" || req.Password == "" {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	id, hash, isAdmin, roles, err := h.Users.GetByUsername(r.Context(), req.Username)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if !VerifyPassword(hash, req.Password) {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	if err := h.Sessions.IssueSessionCookie(w, id, req.Username, roles); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(struct {
		User userResponse `json:"user"`
	}{
		// This login path is exclusively for built-in (username/password)
		// accounts, so auth_provider is always "local" here (an
		// SSO-provisioned user authenticates via /auth/sso/login instead
		// and never reaches LoginHandler).
		User: userResponse{ID: id, Username: req.Username, AuthProvider: "local", Roles: roles, IsAdmin: isAdmin},
	})
}

// LogoutHandler implements POST /api/auth/logout: clears the session cookie
// and responds 204, per contracts/openapi.yaml.
type LogoutHandler struct {
	Sessions *Sessions
}

// NewLogoutHandler constructs a LogoutHandler.
func NewLogoutHandler(sessions *Sessions) *LogoutHandler {
	return &LogoutHandler{Sessions: sessions}
}

func (h *LogoutHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Sessions.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// remoteIP extracts the client IP from r.RemoteAddr, stripping the port
// when present.
func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
