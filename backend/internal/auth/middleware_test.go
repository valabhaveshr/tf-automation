package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestRequireAdminRejectsNonAdminClaims(t *testing.T) {
	claims := &Claims{IsAdmin: false}
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))

	rec := httptest.NewRecorder()
	RequireAdmin(next).ServeHTTP(rec, req)

	if handlerCalled {
		t.Fatal("expected next handler not to be called for a non-admin caller")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestRequireAdminRejectsMissingClaims(t *testing.T) {
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
	})

	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh", nil)
	rec := httptest.NewRecorder()
	RequireAdmin(next).ServeHTTP(rec, req)

	if handlerCalled {
		t.Fatal("expected next handler not to be called when no claims are present")
	}
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected status %d, got %d", http.StatusForbidden, rec.Code)
	}
}

func TestRequireAdminAllowsAdminClaims(t *testing.T) {
	claims := &Claims{IsAdmin: true}
	handlerCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/catalog/refresh", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))

	rec := httptest.NewRecorder()
	RequireAdmin(next).ServeHTTP(rec, req)

	if !handlerCalled {
		t.Fatal("expected next handler to be called for an admin caller")
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestRequireAuthRejectsMissingCookie(t *testing.T) {
	s := New([]byte("test-secret"))
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not be called without a valid session cookie")
	})

	req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	rec := httptest.NewRecorder()
	s.RequireAuth(next).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
}

func TestRequireAuthInjectsClaimsOnValidCookie(t *testing.T) {
	s := New([]byte("test-secret"))
	userID := uuid.New()

	rec := httptest.NewRecorder()
	if err := s.IssueSessionCookie(rec, userID, "erin", nil); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}
	cookie := rec.Result().Cookies()[0]

	var gotClaims *Claims
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims, ok := UserFromContext(r.Context())
		if !ok {
			t.Fatal("expected claims to be present in request context")
		}
		gotClaims = claims
	})

	req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	req.AddCookie(cookie)

	rr := httptest.NewRecorder()
	s.RequireAuth(next).ServeHTTP(rr, req)

	if gotClaims == nil {
		t.Fatal("expected claims to have been captured")
	}
	if gotClaims.UserID != userID || gotClaims.Username != "erin" {
		t.Fatalf("unexpected claims: %+v", gotClaims)
	}
}
