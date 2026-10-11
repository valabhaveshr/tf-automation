package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/catalog"
	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/naming"
	"tf-automation/backend/internal/output"
)

// Server holds every dependency WS-G's HTTP handlers need: the real WS-A
// Store, WS-D catalog Service (+ its webhook handler), WS-I naming Engine,
// WS-E output Client, and WS-F Sessions. main.go constructs one of these
// from real, non-mock implementations and calls Routes() to get the
// http.Handler to serve.
type Server struct {
	Store    *data.Store
	Catalog  *catalog.Service
	Webhook  *catalog.WebhookHandler
	Naming   *naming.Engine
	Output   *output.Client
	Sessions *auth.Sessions

	// SSO is P3-B's OIDC provider, constructed by main.go and left nil
	// when SSO isn't configured (empty OIDC_ISSUER_URL -- see
	// auth.NewOIDCProvider's doc comment). Routes() only mounts
	// /auth/sso/login and /auth/sso/callback when this is non-nil, so the
	// server stays deployable password-only during the SSO migration
	// window per contracts/openapi.yaml.
	SSO *auth.OIDCProvider

	// OutputRepoOwner/OutputRepoName identify the output GitHub repo
	// records are committed to.
	OutputRepoOwner string
	OutputRepoName  string

	// ModulesRepoOwner/ModulesRepoName/ModulesPath feed
	// codegen.RecordInput so generated `module.source` URLs point at the
	// real modules repo and directory.
	ModulesRepoOwner string
	ModulesRepoName  string
	ModulesPath      string

	// StateBackendType/StateBackendBucket configure the one-time
	// backend.tf generated at first save (codegen.RenderBackendFile).
	StateBackendType   string
	StateBackendBucket string
}

// Routes builds the full chi router matching contracts/openapi.yaml's
// path/method surface exactly. Middleware is applied per the OpenAPI
// spec's per-path security requirements: the document sets a top-level
// default of `security: [sessionAuth: []]` for every operation, overridden
// to `security: []` (no auth) only on POST /auth/login and
// POST /webhooks/github/modules (HMAC-verified instead, per WS-D). Every
// other path -- including the two GET /catalog* reads, which have no
// per-operation override -- therefore requires a valid session per the
// spec, even though they carry no further access-control benefit beyond
// "logged in" (Phase 1 has no per-user catalog scoping). POST /auth/logout
// likewise has no override, but is deliberately left reachable without a
// *valid* session here: it only clears the session cookie (idempotent, no
// data returned), so an already-expired/invalid session can still safely
// "log out" client-side rather than getting a confusing 401 on the one
// endpoint whose whole job is to clear that exact state.
//   - POST /catalog/refresh, /admin/*: RequireAuth + RequireAdmin
//   - /records/*: RequireAuth (Phase 1 has no per-record RBAC yet -- "any
//     authenticated user can edit any record" is an explicitly accepted,
//     documented Phase 1 limitation per plan/ARCHITECTURE.md's Security
//     edge case table)
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()

	loginHandler := auth.NewLoginHandler(s.Sessions, &userLookupAdapter{store: s.Store}, auth.DefaultLoginRateLimiter())
	logoutHandler := auth.NewLogoutHandler(s.Sessions)

	r.Route("/api", func(r chi.Router) {
		r.Post("/auth/login", loginHandler.ServeHTTP)
		r.Post("/auth/logout", logoutHandler.ServeHTTP)
		r.With(s.Sessions.RequireAuth).Get("/auth/me", s.handleAuthMe)

		// P3-B/P3-C: OIDC SSO. Only mounted when SSO is configured (see
		// the SSO field's doc comment) -- these two ARE the auth flow, so
		// no RequireAuth, matching the OpenAPI contract's `security: []`
		// override on both operations.
		if s.SSO != nil {
			r.Get("/auth/sso/login", s.SSO.BeginLogin)
			r.Get("/auth/sso/callback", s.SSO.HandleCallback)
		}

		r.With(s.Sessions.RequireAuth).Get("/catalog", s.handleCatalogList)
		// Wildcard rather than "/catalog/{moduleName}/schema": nested module
		// names contain slashes ("compute/instance"), which a single chi
		// path parameter can't match. handleCatalogSchema parses the rest.
		r.With(s.Sessions.RequireAuth).Get("/catalog/*", s.handleCatalogSchema)

		r.With(s.Sessions.RequireAuth, auth.RequireAdmin).Post("/catalog/refresh", s.handleCatalogRefresh)

		// HMAC-verified instead of session-authenticated, per WS-D.
		r.Post("/webhooks/github/modules", s.handleWebhook)

		// Public liveness/readiness probe for containers and load balancers.
		r.Get("/healthz", s.handleHealthz)

		// Customer tenancy (migration 000003). The two /invite/{token}
		// routes are public: the unguessable token is the credential.
		r.Get("/invite/{token}", s.handlePreviewInvitation)
		r.Post("/invite/{token}/accept", s.handleAcceptInvitation)
		r.With(s.Sessions.RequireAuth).Post("/onboarding", s.handleOnboarding)
		r.Route("/customer", func(r chi.Router) {
			r.Use(s.Sessions.RequireAuth)
			r.Get("/", s.handleGetMyCustomer)
			r.Put("/", s.handleUpdateMyCustomer)
			r.Get("/users", s.handleListTeam)
		})
		r.Route("/invitations", func(r chi.Router) {
			r.Use(s.Sessions.RequireAuth)
			r.Get("/", s.handleListInvitations)
			r.Post("/", s.handleCreateInvitation)
			r.Delete("/{id}", s.handleRevokeInvitation)
		})

		r.Route("/admin", func(r chi.Router) {
			r.Use(s.Sessions.RequireAuth, auth.RequireAdmin)
			r.Get("/customers", s.handleListCustomers)
			r.Post("/customers", s.handleCreateCustomer)
			r.Get("/customers/{id}/users", s.handleAdminListCustomerUsers)
			r.Get("/naming-templates", s.handleListNamingTemplates)
			r.Post("/naming-templates", s.handleSaveNamingTemplate)
			r.Get("/audit-log", s.handleAuditLog)
		})

		r.Route("/records", func(r chi.Router) {
			r.Use(s.Sessions.RequireAuth)
			r.Get("/", s.handleListRecords)
			r.Post("/", s.handleCreateRecord)
			r.Post("/preview", s.handlePreviewCreate)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", s.handleGetRecord)
				r.Put("/", s.handleUpdateRecord)
				r.Delete("/", s.handleDeleteRecord)
				r.Put("/preview", s.handlePreviewEdit)
				r.Get("/sync-status", s.handleSyncStatus)
				r.Post("/retry-sync", s.handleRetrySync)
			})
		})
	})

	return r
}

// httpTimeout is a sane upper bound passed to context deadlines for
// handlers that fan out to GitHub (multiple sequential API calls per
// commit); it is not wired as a hard http.Server timeout here since that is
// main.go's concern (http.Server.ReadTimeout/WriteTimeout), just documented
// for anyone tuning those.
const httpTimeout = 60 * time.Second
