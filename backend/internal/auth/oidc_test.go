package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"testing"
	"time"
)

// These tests cover the pure-logic pieces of the OIDC SSO flow (PKCE
// generation, the state store, and redirect_to allowlisting) that don't
// require a real or mocked OIDC provider. The discovery/exchange/verify
// flow (NewOIDCProvider, BeginLogin's actual redirect target, and
// HandleCallback's token exchange + ID-token verification) needs a real or
// mocked IdP to integration-test and is NOT covered here.

// codeVerifierPattern matches the RFC 7636 §4.1 unreserved character set:
// [A-Z] / [a-z] / [0-9] / "-" / "." / "_" / "~".
var codeVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9\-._~]{43,128}$`)

func TestGenerateCodeVerifier(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		verifier, err := generateCodeVerifier()
		if err != nil {
			t.Fatalf("generateCodeVerifier: %v", err)
		}
		if !codeVerifierPattern.MatchString(verifier) {
			t.Fatalf("verifier %q does not match RFC 7636 charset/length requirements", verifier)
		}
		if seen[verifier] {
			t.Fatalf("generateCodeVerifier produced a duplicate: %q", verifier)
		}
		seen[verifier] = true
	}
}

func TestCodeChallengeS256(t *testing.T) {
	// RFC 7636 Appendix B's worked example.
	const verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	const wantChallenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"

	got := codeChallengeS256(verifier)
	if got != wantChallenge {
		t.Fatalf("codeChallengeS256(%q) = %q, want %q", verifier, got, wantChallenge)
	}
}

func TestCodeChallengeS256_MatchesManualComputation(t *testing.T) {
	verifier, err := generateCodeVerifier()
	if err != nil {
		t.Fatalf("generateCodeVerifier: %v", err)
	}

	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])

	got := codeChallengeS256(verifier)
	if got != want {
		t.Fatalf("codeChallengeS256(%q) = %q, want %q", verifier, got, want)
	}
	// The challenge must be unpadded base64url: no '=', '+', or '/'.
	if regexp.MustCompile(`[=+/]`).MatchString(got) {
		t.Fatalf("challenge %q contains non-base64url-unpadded characters", got)
	}
}

func TestGenerateState_Unique(t *testing.T) {
	a, err := generateState()
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	b, err := generateState()
	if err != nil {
		t.Fatalf("generateState: %v", err)
	}
	if a == b {
		t.Fatalf("generateState produced identical values across two calls: %q", a)
	}
}

func TestStateStore_SingleUse(t *testing.T) {
	store := newStateStore(time.Minute)

	state, err := store.put("verifier-1", "/app/dashboard")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	entry, ok := store.take(state)
	if !ok {
		t.Fatalf("take: expected entry to be found on first read")
	}
	if entry.verifier != "verifier-1" || entry.redirectTo != "/app/dashboard" {
		t.Fatalf("take returned unexpected entry: %+v", entry)
	}

	if _, ok := store.take(state); ok {
		t.Fatalf("take: expected second read of the same state to fail (single-use)")
	}
}

func TestStateStore_UnknownState(t *testing.T) {
	store := newStateStore(time.Minute)
	if _, ok := store.take("never-issued"); ok {
		t.Fatalf("take: expected unknown state to fail")
	}
}

func TestStateStore_Expiry(t *testing.T) {
	store := newStateStore(-time.Second) // already-expired TTL

	state, err := store.put("verifier-1", "/app")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	if _, ok := store.take(state); ok {
		t.Fatalf("take: expected expired entry to fail")
	}
}

func TestStateStore_DistinctStatesDoNotCollide(t *testing.T) {
	store := newStateStore(time.Minute)

	stateA, err := store.put("verifier-a", "/a")
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	stateB, err := store.put("verifier-b", "/b")
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	entryB, ok := store.take(stateB)
	if !ok || entryB.verifier != "verifier-b" {
		t.Fatalf("take(stateB) = %+v, %v", entryB, ok)
	}

	// Consuming stateB must not have affected stateA.
	entryA, ok := store.take(stateA)
	if !ok || entryA.verifier != "verifier-a" {
		t.Fatalf("take(stateA) = %+v, %v", entryA, ok)
	}
}

func TestValidateRedirectTo_AllowedPrefixPasses(t *testing.T) {
	cfg := OIDCConfig{
		DefaultRedirectTo:       "/app",
		AllowedRedirectPrefixes: []string{"/app/", "/dashboard"},
	}

	got := cfg.validateRedirectTo("/app/records/123")
	if got != "/app/records/123" {
		t.Fatalf("validateRedirectTo = %q, want the requested path unchanged", got)
	}

	got = cfg.validateRedirectTo("/dashboard")
	if got != "/dashboard" {
		t.Fatalf("validateRedirectTo = %q, want the requested path unchanged", got)
	}
}

func TestValidateRedirectTo_DisallowedFallsBackToDefault(t *testing.T) {
	cfg := OIDCConfig{
		DefaultRedirectTo:       "/app",
		AllowedRedirectPrefixes: []string{"/app/"},
	}

	cases := []string{
		"https://evil.example/phish",
		"//evil.example/phish",
		"/other/path",
		"",
	}
	for _, redirectTo := range cases {
		got := cfg.validateRedirectTo(redirectTo)
		if got != "/app" {
			t.Fatalf("validateRedirectTo(%q) = %q, want fallback to DefaultRedirectTo %q", redirectTo, got, "/app")
		}
	}
}

func TestValidateRedirectTo_EmptyPrefixInAllowlistNeverMatches(t *testing.T) {
	// A zero-value/empty entry in AllowedRedirectPrefixes must not act as a
	// wildcard that allows any redirect_to (strings.HasPrefix(x, "") is
	// always true, so this guards against that footgun).
	cfg := OIDCConfig{
		DefaultRedirectTo:       "/app",
		AllowedRedirectPrefixes: []string{""},
	}

	got := cfg.validateRedirectTo("https://evil.example/phish")
	if got != "/app" {
		t.Fatalf("validateRedirectTo with empty-string allowlist entry = %q, want fallback to /app", got)
	}
}
