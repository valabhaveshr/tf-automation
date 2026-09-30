package main

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/oauth2"

	"tf-automation/backend/internal/config"
)

// buildGitHubTransport is the "small piece of GitHub auth-token wiring"
// this task calls out as belonging in main.go, not WS-D or WS-E (neither
// was scoped to own credential acquisition -- both accept an already-
// authenticated *github.Client/http.RoundTripper from their caller).
//
// It supports the two credential modes plan/ARCHITECTURE.md's locked
// "GitHub credentials" decision covers: a GitHub App installation (the
// primary, higher-rate-limit choice) or a plain PAT (the documented
// interim/test fallback). Both ultimately just produce an
// http.RoundTripper that stamps outgoing requests with a bearer token --
// go-github's client is transport-agnostic, so the same RoundTripper can
// back both the catalog's GitHubSource (modules repo) and output.Client
// (output repo).
func buildGitHubTransport(cfg *config.Config) (http.RoundTripper, error) {
	if cfg.GitHubToken != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: cfg.GitHubToken})
		return oauth2.NewClient(context.Background(), ts).Transport, nil
	}

	privKey, err := jwt.ParseRSAPrivateKeyFromPEM(cfg.GitHubAppPrivateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("main: parsing GITHUB_APP_PRIVATE_KEY: %w", err)
	}

	return &githubAppTransport{
		appID:          cfg.GitHubAppID,
		installationID: cfg.GitHubAppInstallationID,
		privateKey:     privKey,
	}, nil
}

// githubAppTransport is a minimal, self-contained GitHub App
// installation-token transport: it mints a short-lived App JWT (RS256,
// signed with the App's private key via golang-jwt -- already a project
// dependency, used elsewhere for session cookies), exchanges it for an
// installation access token via GitHub's REST API directly (plain
// net/http + encoding/json), and caches that token until shortly before it
// expires (installation tokens are valid for 1 hour).
//
// This deliberately avoids adding a new dependency (e.g.
// bradleyfalzon/ghinstallation) given this environment has no Go toolchain
// to fetch/verify one; every piece here is either stdlib or an
// already-vetted, already-used dependency.
type githubAppTransport struct {
	appID          string
	installationID string
	privateKey     *rsa.PrivateKey
	base           http.RoundTripper

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func (t *githubAppTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.ensureToken(req.Context())
	if err != nil {
		return nil, fmt.Errorf("main: github app: acquiring installation token: %w", err)
	}
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+tok)
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req2)
}

func (t *githubAppTransport) ensureToken(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.token != "" && time.Until(t.expiresAt) > 5*time.Minute {
		return t.token, nil
	}

	appJWT, err := t.mintAppJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://api.github.com/app/installations/%s/access_tokens", t.installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("github app installation token request failed: status %d", resp.StatusCode)
	}

	var body struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decoding installation token response: %w", err)
	}

	t.token = body.Token
	t.expiresAt = body.ExpiresAt
	return t.token, nil
}

// mintAppJWT builds a short-lived (9 minute; GitHub allows up to 10)
// RS256-signed JWT authenticating as the App itself, per GitHub's
// documented App-authentication scheme
// (https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app).
func (t *githubAppTransport) mintAppJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		IssuedAt:  jwt.NewNumericDate(now.Add(-30 * time.Second)), // clock-drift allowance, per GitHub's docs
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
		Issuer:    t.appID,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(t.privateKey)
}
