package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSessionsIssueAndParseRoundTrip(t *testing.T) {
	s := New([]byte("test-secret-please-ignore"))
	userID := uuid.New()

	rec := httptest.NewRecorder()
	if err := s.IssueSessionCookie(rec, userID, "alice", []string{"admin"}); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName {
		t.Fatalf("unexpected cookie name %q, want %q", cookie.Name, SessionCookieName)
	}
	if !cookie.HttpOnly {
		t.Error("expected cookie to be HttpOnly")
	}
	if !cookie.Secure {
		t.Error("expected cookie to be Secure")
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", cookie.SameSite)
	}

	claims, err := s.Parse(cookie.Value)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.UserID != userID {
		t.Errorf("UserID = %v, want %v", claims.UserID, userID)
	}
	if claims.Username != "alice" {
		t.Errorf("Username = %q, want %q", claims.Username, "alice")
	}
	if !claims.IsAdmin {
		t.Error("expected IsAdmin to round-trip as true")
	}
}

func TestSessionsParseRejectsExpiredToken(t *testing.T) {
	s := New([]byte("test-secret")).WithTTL(-1 * time.Minute)

	rec := httptest.NewRecorder()
	if err := s.IssueSessionCookie(rec, uuid.New(), "bob", nil); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}
	cookie := rec.Result().Cookies()[0]

	if _, err := s.Parse(cookie.Value); err == nil {
		t.Fatal("expected an already-expired token to fail Parse")
	}
}

func TestSessionsParseRejectsTamperedToken(t *testing.T) {
	s := New([]byte("test-secret"))

	rec := httptest.NewRecorder()
	if err := s.IssueSessionCookie(rec, uuid.New(), "carol", nil); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}
	cookie := rec.Result().Cookies()[0]

	// Flip the last character of the signature to simulate tampering.
	tampered := cookie.Value[:len(cookie.Value)-1] + "x"
	if tampered == cookie.Value {
		tampered = cookie.Value[:len(cookie.Value)-1] + "y"
	}

	if _, err := s.Parse(tampered); err == nil {
		t.Fatal("expected a tampered token to fail Parse")
	}
}

func TestSessionsParseRejectsWrongSecret(t *testing.T) {
	issuer := New([]byte("secret-a"))
	verifier := New([]byte("secret-b"))

	rec := httptest.NewRecorder()
	if err := issuer.IssueSessionCookie(rec, uuid.New(), "dave", nil); err != nil {
		t.Fatalf("IssueSessionCookie: %v", err)
	}
	cookie := rec.Result().Cookies()[0]

	if _, err := verifier.Parse(cookie.Value); err == nil {
		t.Fatal("expected a token signed with a different secret to fail Parse")
	}
}
