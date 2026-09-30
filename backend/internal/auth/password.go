// Package auth implements Phase 1's "simple built-in username/password"
// authentication (see plan/ARCHITECTURE.md, Locked Decisions -> "Auth
// (Phase 1)"): password hashing, a stateless signed-cookie session scheme,
// auth/admin middleware, login rate-limiting, and the login/logout HTTP
// handlers. It deliberately does not depend on WS-A's concrete data-access
// package (see UserLookup in login.go) so it can be built and tested in
// isolation, per Phase 0's parallel-workstream contract.
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword hashes a plaintext password using bcrypt with a
// package-default cost factor. The returned string is safe to store as
// users.password_hash.
func HashPassword(plain string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword reports whether plain matches the given bcrypt hash. It
// returns false (never an error) on any mismatch, including a malformed
// hash, so callers can treat it as a simple boolean check without leaking
// details about why verification failed.
func VerifyPassword(hash, plain string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain))
	return err == nil
}
