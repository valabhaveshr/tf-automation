package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeUserLookup struct {
	id           uuid.UUID
	username     string
	passwordHash string
	isAdmin      bool
	roles        []string
}

func (f fakeUserLookup) GetByUsername(ctx context.Context, username string) (uuid.UUID, string, bool, []string, error) {
	if username != f.username {
		return uuid.UUID{}, "", false, nil, ErrUserNotFound
	}
	return f.id, f.passwordHash, f.isAdmin, f.roles, nil
}

func newTestLoginHandler(t *testing.T) (*LoginHandler, uuid.UUID) {
	t.Helper()
	hash, err := HashPassword("s3cret-pw")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	userID := uuid.New()
	lookup := fakeUserLookup{id: userID, username: "frank", passwordHash: hash, isAdmin: true, roles: []string{"admin", "requester"}}
	handler := NewLoginHandler(New([]byte("test-secret")), lookup, NewRateLimiter(3, time.Minute))
	return handler, userID
}

func doLogin(h *LoginHandler, username, password string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(loginRequest{Username: username, Password: password})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestLoginHandlerSuccess(t *testing.T) {
	h, userID := newTestLoginHandler(t)

	rec := doLogin(h, "frank", "s3cret-pw")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != SessionCookieName {
		t.Fatalf("expected a %s cookie to be set, got %+v", SessionCookieName, cookies)
	}

	var resp struct {
		User struct {
			ID       uuid.UUID `json:"id"`
			Username string    `json:"username"`
			IsAdmin  bool      `json:"is_admin"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.User.ID != userID || resp.User.Username != "frank" || !resp.User.IsAdmin {
		t.Fatalf("unexpected user in response: %+v", resp.User)
	}
}

func TestLoginHandlerWrongPassword(t *testing.T) {
	h, _ := newTestLoginHandler(t)

	rec := doLogin(h, "frank", "wrong-password")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("expected no session cookie to be set on failed login")
	}
}

func TestLoginHandlerUnknownUser(t *testing.T) {
	h, _ := newTestLoginHandler(t)

	rec := doLogin(h, "nobody", "whatever")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestLoginHandlerRateLimited(t *testing.T) {
	h, _ := newTestLoginHandler(t)

	// Exhaust the 3-attempt budget configured in newTestLoginHandler with
	// failed attempts, then confirm the next attempt is rejected even with
	// the correct password.
	for i := 0; i < 3; i++ {
		doLogin(h, "frank", "wrong-password")
	}

	rec := doLogin(h, "frank", "s3cret-pw")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429 after exceeding the rate limit, got %d", rec.Code)
	}
}
