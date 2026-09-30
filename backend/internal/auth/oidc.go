// This file (P3-B) adds generic OIDC SSO login as an alternative/primary
// login path alongside Phase 1's built-in username/password login
// (login.go), per contracts/openapi.yaml's `/auth/sso/login` and
// `/auth/sso/callback` (GET, redirect-based, PKCE + state). It works
// against any standards-compliant OIDC provider discovered via its issuer
// URL (Azure AD/Entra, Okta, Google Workspace, Auth0, Keycloak, ...) --
// that choice (generic OIDC over SAML or a vendor-specific integration) is
// locked, see plan/ARCHITECTURE.md.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

// OIDCConfig configures a single OIDC identity provider integration.
type OIDCConfig struct {
	// IssuerURL is the IdP's OIDC issuer, used for discovery
	// (`<IssuerURL>/.well-known/openid-configuration`). Left empty, SSO is
	// considered not configured -- see NewOIDCProvider.
	IssuerURL string

	// ClientID / ClientSecret are this app's registration with the IdP.
	ClientID     string
	ClientSecret string

	// RedirectURL is the callback URL registered with the IdP -- must
	// match the deployment's `/auth/sso/callback` absolute URL exactly.
	RedirectURL string

	// DefaultRedirectTo is the frontend path landed on after a successful
	// login when the request's redirect_to is absent or fails the
	// allowlist check below.
	DefaultRedirectTo string

	// AllowedRedirectPrefixes allowlists frontend paths the `redirect_to`
	// query param (on /auth/sso/login) may target, per the OpenAPI
	// contract's note that it is "validated against an allowlist
	// server-side" -- this is what prevents it being used as an open
	// redirect. A redirect_to is accepted iff it has one of these strings
	// as a prefix; anything else (including any absolute
	// "https://evil.example/..." URL, as long as none of the allowed
	// prefixes is itself unsafely permissive) falls back to
	// DefaultRedirectTo.
	AllowedRedirectPrefixes []string

	// StateTTL bounds how long a begun login (state + PKCE verifier) stays
	// valid before HandleCallback rejects it as expired. Defaults to 10
	// minutes when zero.
	StateTTL time.Duration
}

// validateRedirectTo returns redirectTo if it is non-empty and allowlisted,
// otherwise cfg.DefaultRedirectTo.
func (cfg OIDCConfig) validateRedirectTo(redirectTo string) string {
	if redirectTo == "" {
		return cfg.DefaultRedirectTo
	}
	for _, prefix := range cfg.AllowedRedirectPrefixes {
		if prefix != "" && strings.HasPrefix(redirectTo, prefix) {
			return redirectTo
		}
	}
	return cfg.DefaultRedirectTo
}

// OIDCProvider wraps a discovered *oidc.Provider and an oauth2.Config for a
// single configured IdP, plus the in-memory PKCE/state store used to bind
// BeginLogin to its later HandleCallback.
type OIDCProvider struct {
	cfg          OIDCConfig
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	oauth2Config oauth2.Config
	states       *stateStore

	// Sessions issues the session cookie once a callback authenticates
	// successfully. It must be set by the caller after construction (e.g.
	// during WS-G's HTTP wiring) -- NewOIDCProvider's signature is fixed to
	// (ctx, cfg) so it can't be a constructor parameter.
	Sessions *Sessions

	// Users gets-or-creates the local user record for a verified OIDC
	// identity and returns their roles. Must be set by the caller after
	// construction; P3-C supplies the concrete implementation (backed by
	// P3-A's roles table) once it exists -- this file is written against
	// the UserUpserter interface now rather than waiting on that.
	Users UserUpserter
}

// NewOIDCProvider performs OIDC discovery against cfg.IssuerURL and
// constructs an OIDCProvider ready to serve BeginLogin/HandleCallback (once
// its Sessions and Users fields are set).
//
// Phase 3 must stay deployable with SSO not yet configured, falling back to
// password-only login (per the migration window documented in
// contracts/openapi.yaml). To support that without forcing every caller to
// distinguish "real error" from "just not configured", an empty
// cfg.IssuerURL is NOT an error: NewOIDCProvider returns (nil, nil) --
// meaning "SSO disabled" -- rather than a sentinel error value. Callers
// should write:
//
//	sso, err := auth.NewOIDCProvider(ctx, cfg)
//	if err != nil { /* IdP unreachable/misconfigured: fail startup */ }
//	if sso != nil { /* mount /auth/sso/login and /auth/sso/callback */ }
func NewOIDCProvider(ctx context.Context, cfg OIDCConfig) (*OIDCProvider, error) {
	if cfg.IssuerURL == "" {
		return nil, nil
	}

	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, err
	}

	ttl := cfg.StateTTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}

	return &OIDCProvider{
		cfg:      cfg,
		provider: provider,
		verifier: provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2Config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
		},
		states: newStateStore(ttl),
	}, nil
}

// --- PKCE (RFC 7636) ---------------------------------------------------
//
// go-oidc deliberately doesn't include PKCE helpers (it's an ID-token
// verification library, not an OAuth2 client helper beyond what
// golang.org/x/oauth2 already gives). The verifier/challenge generation
// here is the standard, well-documented RFC 7636 recipe.

// generateCodeVerifier returns a cryptographically random PKCE code
// verifier per RFC 7636 §4.1: 43-128 characters drawn from
// [A-Z a-z 0-9 - . _ ~]. Base64url-no-padding-encoding 32 random bytes
// yields a 43-character string whose alphabet (base64url minus padding) is
// a subset of that unreserved set, satisfying both the length and charset
// requirements in one step.
func generateCodeVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// codeChallengeS256 computes the RFC 7636 §4.2 S256 code_challenge for the
// given verifier: BASE64URL-ENCODE(SHA256(ASCII(verifier))), without
// padding.
func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// generateState returns a cryptographically random opaque value used to
// bind an authorization request to its callback (CSRF mitigation).
func generateState() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// --- server-side state store --------------------------------------------

// pendingLogin is what a begun /auth/sso/login records, keyed by its random
// state value, for HandleCallback to look up.
type pendingLogin struct {
	verifier   string
	redirectTo string
	expiresAt  time.Time
}

// stateStore holds in-flight OIDC login attempts (state -> PKCE verifier +
// redirect_to) between BeginLogin and HandleCallback. Each entry is
// single-use: take() deletes it on read, so a replayed/reused state or
// authorization code can't be used to complete a second login.
//
// IMPORTANT (Phase 3 limitation, documented not accidental -- the same
// single-instance caveat RateLimiter documents for its own in-memory map in
// ratelimit.go): this is a single-instance, in-memory, mutex-guarded map.
// It is reset on restart and is NOT shared across multiple backend replicas
// behind a load balancer -- a login begun on one replica will fail its
// callback if the callback request is routed to a different replica.
// Acceptable for Phase 3's single-instance deployment target; if/when this
// service is horizontally scaled, this needs to move to a shared store
// (e.g. Redis with a TTL) same as the login rate limiter.
//
// There is no background sweeper goroutine; put() opportunistically sweeps
// expired entries on each call, matching RateLimiter's lazy-sweep-on-access
// approach, so the map doesn't grow unboundedly in a long-running process.
type stateStore struct {
	mu      sync.Mutex
	entries map[string]pendingLogin
	ttl     time.Duration
}

func newStateStore(ttl time.Duration) *stateStore {
	return &stateStore{entries: make(map[string]pendingLogin), ttl: ttl}
}

// put generates a fresh random state, records verifier/redirectTo under it
// with an expiry of ttl from now, and returns the state.
func (s *stateStore) put(verifier, redirectTo string) (string, error) {
	state, err := generateState()
	if err != nil {
		return "", err
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range s.entries {
		if now.After(v.expiresAt) {
			delete(s.entries, k)
		}
	}

	s.entries[state] = pendingLogin{
		verifier:   verifier,
		redirectTo: redirectTo,
		expiresAt:  now.Add(s.ttl),
	}
	return state, nil
}

// take looks up and deletes (single-use) the pending login for state. ok is
// false if state is unknown, already consumed, or expired -- callers must
// treat all three identically (generic 401), same rationale as
// ErrInvalidSession in session.go.
func (s *stateStore) take(state string) (pendingLogin, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, found := s.entries[state]
	delete(s.entries, state)
	if !found || time.Now().After(entry.expiresAt) {
		return pendingLogin{}, false
	}
	return entry, true
}

// --- handlers -------------------------------------------------------------

// BeginLogin implements GET /auth/sso/login: generates a PKCE verifier +
// S256 challenge and a random state, records them server-side alongside the
// (allowlist-validated) redirect_to, and redirects (302) to the IdP's
// authorization endpoint requesting the openid/email/profile scopes.
func (p *OIDCProvider) BeginLogin(w http.ResponseWriter, r *http.Request) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	redirectTo := p.cfg.validateRedirectTo(r.URL.Query().Get("redirect_to"))

	state, err := p.states.put(verifier, redirectTo)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	authURL := p.oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("code_challenge", codeChallengeS256(verifier)),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// OIDCClaims are the standard ID token claims HandleCallback extracts:
// subject (the IdP's stable, durable identifier for the user -- this, not
// username/email, is what UserUpserter should key the local identity on,
// since username/email can change at the IdP) and a display username,
// preferring preferred_username (commonly populated by Azure AD/Entra and
// Okta) and falling back to email (e.g. Google Workspace, which typically
// doesn't send preferred_username).
type OIDCClaims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
}

// Username returns PreferredUsername if set, else Email.
func (c OIDCClaims) Username() string {
	if c.PreferredUsername != "" {
		return c.PreferredUsername
	}
	return c.Email
}

// UserUpserter is the minimal interface HandleCallback needs to turn a
// verified OIDC identity into a local user record and its roles. It's
// defined here, not imported from WS-A's/P3-A's concrete data package, so
// this workstream stays decoupled and independently testable per Phase 0's
// parallel-workstream contract (mirroring UserLookup in login.go). P3-C
// implements it against the data layer once P3-A's roles table exists;
// this file is written against the interface now rather than waiting on
// that.
type UserUpserter interface {
	// UpsertSSOUser gets-or-creates a local user for the given IdP subject
	// and returns that user's id and current roles. Implementations should
	// treat subject (not username) as the durable identity key, and should
	// update the stored username if it has changed at the IdP.
	UpsertSSOUser(ctx context.Context, subject, username string) (userID uuid.UUID, roles []string, err error)
}

// errSSOAuthFailed is the generic error surfaced to the client on any
// callback failure (bad/expired/unknown state, code exchange failure, ID
// token verification failure, or upsert failure). Deliberately generic,
// per the task spec: never reflect raw IdP/library error details -- which
// can include internal provider diagnostics -- back to the client.
var errSSOAuthFailed = errors.New("auth: sso authentication failed")

// HandleCallback implements GET /auth/sso/callback: reads code+state,
// looks up and single-use-consumes the matching pending login, exchanges
// the code for tokens (with the PKCE verifier), verifies the ID token,
// upserts the local user via Users, issues a session cookie via Sessions,
// and redirects (302) to the stored redirect_to. Any failure responds 401
// with a generic body (errSSOAuthFailed).
func (p *OIDCProvider) HandleCallback(w http.ResponseWriter, r *http.Request) {
	if p.Sessions == nil || p.Users == nil {
		// Misconfiguration (caller forgot to wire Sessions/Users after
		// NewOIDCProvider), not a client-supplied-bad-input case -- still
		// never leaks internals, just distinguishes 500 from 401.
		http.Error(w, "sso not configured", http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	pending, ok := p.states.take(state)
	if !ok {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	token, err := p.oauth2Config.Exchange(ctx, code,
		oauth2.SetAuthURLParam("code_verifier", pending.verifier),
	)
	if err != nil {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	var claims OIDCClaims
	if err := idToken.Claims(&claims); err != nil || claims.Subject == "" {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	username := claims.Username()
	if username == "" {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	userID, roles, err := p.Users.UpsertSSOUser(ctx, claims.Subject, username)
	if err != nil {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	if err := p.Sessions.IssueSessionCookie(w, userID, username, roles); err != nil {
		http.Error(w, errSSOAuthFailed.Error(), http.StatusUnauthorized)
		return
	}

	redirectTo := pending.redirectTo
	if redirectTo == "" {
		redirectTo = p.cfg.DefaultRedirectTo
	}
	http.Redirect(w, r, redirectTo, http.StatusFound)
}
