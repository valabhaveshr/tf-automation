package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
)

// These tests exercise the router's auth/admin middleware wiring only --
// deliberately NOT the handlers behind it. A *Server with nil Store/
// Naming/Output/Catalog is safe to route requests through as long as every
// case here is rejected by auth.Sessions.RequireAuth or auth.RequireAdmin
// before reaching a handler that would dereference those nil fields; that
// is itself the property under test (the middleware must short-circuit).
//
// This environment has no Postgres/GitHub available to stand up a fuller
// integration test (see the task report for what a real Postgres would
// additionally let this suite cover: full preview/create/update round-
// trips and the optimistic-concurrency 409 path, both of which need
// data.Store's real pgxpool-backed methods and naming.Engine's -- which is
// only constructible over a concrete *data.Store, not an interface -- to
// actually execute).

func newTestSessions() *auth.Sessions {
	return auth.New([]byte("test-secret-for-unit-tests-only"))
}

func issueCookie(t *testing.T, sessions *auth.Sessions, isAdmin bool) *http.Cookie {
	t.Helper()
	rec := httptest.NewRecorder()
	var roles []string
	if isAdmin {
		roles = []string{"admin"}
	}
	if err := sessions.IssueSessionCookie(rec, uuid.New(), "testuser", roles); err != nil {
		t.Fatalf("issuing session cookie: %v", err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("IssueSessionCookie did not set a cookie")
	}
	return cookies[0]
}

func TestProtectedEndpoints_401WithoutSession(t *testing.T) {
	srv := &Server{Sessions: newTestSessions()}
	handler := srv.Routes()

	endpoints := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/catalog/refresh"},
		{http.MethodGet, "/api/admin/customers"},
		{http.MethodPost, "/api/admin/customers"},
		{http.MethodGet, "/api/admin/naming-templates"},
		{http.MethodPost, "/api/admin/naming-templates"},
		{http.MethodGet, "/api/records"},
		{http.MethodPost, "/api/records"},
		{http.MethodPost, "/api/records/preview"},
		{http.MethodGet, "/api/catalog"},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s with no session: got %d, want 401", ep.method, ep.path, rec.Code)
		}
	}
}

func TestAdminEndpoints_403ForNonAdmin(t *testing.T) {
	sessions := newTestSessions()
	srv := &Server{Sessions: sessions}
	handler := srv.Routes()

	cookie := issueCookie(t, sessions, false) // authenticated, but not an admin

	endpoints := []struct {
		method, path string
	}{
		{http.MethodPost, "/api/catalog/refresh"},
		{http.MethodGet, "/api/admin/customers"},
		{http.MethodPost, "/api/admin/customers"},
		{http.MethodGet, "/api/admin/naming-templates"},
		{http.MethodPost, "/api/admin/naming-templates"},
	}

	for _, ep := range endpoints {
		req := httptest.NewRequest(ep.method, ep.path, nil)
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as non-admin: got %d, want 403", ep.method, ep.path, rec.Code)
		}
	}
}

func TestAuthLogout_WorksWithoutASession(t *testing.T) {
	srv := &Server{Sessions: newTestSessions()}
	handler := srv.Routes()

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("POST /api/auth/logout with no session: got %d, want 204", rec.Code)
	}
}
