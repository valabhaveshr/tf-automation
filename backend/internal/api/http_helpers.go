package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"

	"tf-automation/backend/internal/auth"
)

// maxRequestBodyBytes bounds request-body reads (JSON bodies and webhook
// payloads) so a malicious/misbehaving client can't exhaust memory via an
// unbounded body.
const maxRequestBodyBytes = 5 << 20 // 5 MiB

func readAllLimited(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
}

// decodeJSON decodes the request body into v, bounding its size.
func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	return dec.Decode(v)
}

// currentUsername extracts the authenticated caller's username from the
// request context, populated by auth.Sessions.RequireAuth. Handlers
// reachable only behind RequireAuth can rely on this always succeeding;
// the fallback is defensive only.
func currentUsername(r *http.Request) string {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		return ""
	}
	return claims.Username
}

func currentUserID(r *http.Request) uuid.UUID {
	claims, ok := auth.UserFromContext(r.Context())
	if !ok {
		return uuid.Nil
	}
	return claims.UserID
}
