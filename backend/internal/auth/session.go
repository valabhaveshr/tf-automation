package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SessionCookieName matches the `sessionAuth` security scheme frozen in
// contracts/openapi.yaml (`apiKey` in a cookie named `tf_automation_session`).
const SessionCookieName = "tf_automation_session"

// DefaultSessionTTL is how long an issued session cookie is valid for.
const DefaultSessionTTL = 12 * time.Hour

// ErrInvalidSession is returned by Parse/FromRequest whenever the session
// cookie is missing, malformed, tampered with, expired, or signed with an
// unexpected algorithm. Callers should treat all of these identically
// (respond 401) rather than distinguishing the cause, to avoid leaking
// signal to an attacker.
var ErrInvalidSession = errors.New("auth: invalid or expired session")

// Claims are the JWT claims embedded in the session cookie. Phase 1 issued
// just user_id, username, is_admin; Phase 3 (P3-B, OIDC SSO) adds a `roles`
// claim carrying the full role set (see contracts/openapi.yaml's User.roles:
// requester, admin, approver) now that SSO-provisioned users can carry more
// than a single boolean flag. IsAdmin is retained and kept populated as a
// computed convenience ("admin" in Roles) for backward compatibility with
// existing callers (e.g. RequireAdmin) that only ever checked it.
type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	IsAdmin  bool      `json:"is_admin"`
	Roles    []string  `json:"roles"`
	jwt.RegisteredClaims
}

// containsRole reports whether role appears in roles.
func containsRole(roles []string, role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// Sessions issues and validates signed, stateless session cookies backed by
// JWT (HS256).
//
// Why stateless JWT rather than a server-side session store: Phase 1 has no
// shared cache/store (e.g. Redis) provisioned as part of the stack, and a
// signed cookie lets any backend instance validate a session without a DB
// round-trip or shared state. The trade-off -- a session can't be revoked
// before it expires -- is acceptable given the short (12h) default TTL; real
// server-side revocation can be added in Phase 3 alongside the full role
// model if needed.
//
// The signing secret is injected via New rather than kept as a package
// global so WS-G can wire in the real secret from config/env without this
// package reaching out for it itself (and so tests can use their own
// throwaway secret).
type Sessions struct {
	secret []byte
	ttl    time.Duration
	// insecureCookies drops the Secure flag from the session cookie. Only
	// for test deployments served over plain HTTP on a non-localhost
	// address, where browsers otherwise discard the cookie.
	insecureCookies bool
}

// New constructs a Sessions signer/verifier using the given HMAC secret.
func New(secret []byte) *Sessions {
	return &Sessions{secret: secret, ttl: DefaultSessionTTL}
}

// WithTTL returns a copy of s using a custom session TTL. Primarily useful
// in tests that need to exercise expiry without sleeping for hours.
func (s *Sessions) WithTTL(ttl time.Duration) *Sessions {
	out := *s
	out.ttl = ttl
	return &out
}

// WithSecureCookies returns a copy of s that sets the session cookie's
// Secure flag only if secure is true (the default). See insecureCookies.
func (s *Sessions) WithSecureCookies(secure bool) *Sessions {
	out := *s
	out.insecureCookies = !secure
	return &out
}

func (s *Sessions) sign(userID uuid.UUID, username string, roles []string) (string, time.Time, error) {
	now := time.Now()
	expiresAt := now.Add(s.ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		IsAdmin:  containsRole(roles, "admin"),
		Roles:    roles,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			Subject:   userID.String(),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	return signed, expiresAt, err
}

// IssueSessionCookie signs a session JWT for the given user and sets it as
// the response's session cookie: HttpOnly, Secure, SameSite=Lax, named
// tf_automation_session per the OpenAPI sessionAuth scheme.
//
// BREAKING CHANGE (P3-B): this used to take a trailing `isAdmin bool`. It now
// takes `roles []string` (the full role set, per contracts/openapi.yaml's
// User.roles) so SSO-provisioned users -- which may carry more than a single
// admin/non-admin flag -- can be represented. IsAdmin is now computed
// internally as `contains(roles, "admin")` and still populated on Claims for
// callers that only check that field. Existing callers that used to pass
// `isAdmin bool` must change to pass `[]string{"admin"}` / `nil` (or
// whatever their real role set is) instead -- see this package's other
// workstreams' call sites for what needs updating.
func (s *Sessions) IssueSessionCookie(w http.ResponseWriter, userID uuid.UUID, username string, roles []string) error {
	signed, expiresAt, err := s.sign(userID, username, roles)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    signed,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   !s.insecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// ClearSessionCookie overwrites the session cookie with an already-expired,
// empty one, effectively logging the caller out (there is no server-side
// session state to invalidate under this stateless scheme).
func (s *Sessions) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !s.insecureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// Parse validates a raw session token string (the cookie's value) and
// returns its claims, or ErrInvalidSession if it doesn't verify.
func (s *Sessions) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidSession
		}
		return s.secret, nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, ErrInvalidSession
	}
	return claims, nil
}

// FromRequest extracts and validates the session cookie from an incoming
// request.
func (s *Sessions) FromRequest(r *http.Request) (*Claims, error) {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, ErrInvalidSession
	}
	return s.Parse(cookie.Value)
}
