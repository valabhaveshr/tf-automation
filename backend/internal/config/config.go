// Package config implements WS-G's environment-based configuration loading
// for the backend server: Postgres connection string, JWT/session signing
// secret, GitHub credentials (either a GitHub App installation or a plain
// PAT, per plan/ARCHITECTURE.md's locked "GitHub credentials: GitHub App"
// decision -- a PAT is accepted as the documented Phase-0-era interim
// fallback), the modules-repo and output-repo owner/name, the modules
// subdirectory convention, the webhook secret + tracked ref, and the
// STATE_BACKEND type/bucket convention used to generate backend.tf.
//
// Nothing in this package talks to the network or a database; Load only
// reads environment variables and validates presence/shape, so main.go can
// fail fast with a clear message before constructing anything.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Config is every environment-derived setting the backend server needs at
// startup.
type Config struct {
	// HTTPAddr is the address http.ListenAndServe binds to, e.g. ":8080".
	HTTPAddr string

	// DatabaseURL is a standard Postgres connection string, passed directly
	// to data.Connect (e.g. "postgres://user:pass@host:5432/db?sslmode=disable").
	DatabaseURL string

	// JWTSigningSecret is the HMAC secret used by auth.New to sign/verify
	// session cookies. Must be non-empty; there is no safe default.
	JWTSigningSecret []byte

	// GitHubToken is a plain PAT. If set, it takes precedence over the
	// GitHub App credentials below -- the simpler of the two supported auth
	// modes, matching plan/ARCHITECTURE.md's Phase 0 note that WS-D/WS-E
	// "can develop and test against a throwaway test org/repo with a
	// temporary App or even a PAT, swapping to the real App's credentials
	// at consolidation time."
	GitHubToken string

	// GitHub App credentials, used when GitHubToken is empty. All three
	// must be set together.
	GitHubAppID             string
	GitHubAppInstallationID string
	// GitHubAppPrivateKeyPEM is the App's PEM-encoded RSA private key
	// contents. May be supplied directly via GITHUB_APP_PRIVATE_KEY (with
	// literal "\n" sequences allowed in the env var, since many env-var
	// mechanisms can't hold real newlines) or read from the file named by
	// GITHUB_APP_PRIVATE_KEY_PATH.
	GitHubAppPrivateKeyPEM []byte

	// ModulesRepoOwner/ModulesRepoName identify the modules (input) repo
	// WS-D's catalog service reads tags/files from.
	ModulesRepoOwner string
	ModulesRepoName  string

	// ModulesPath is the repo-relative directory containing module folders
	// (see plan/ARCHITECTURE.md's "modules/<name>/..." convention).
	// Defaults to "modules".
	ModulesPath string

	// OutputRepoOwner/OutputRepoName identify the generated-code (output)
	// repo WS-E commits rendered Terraform files to.
	OutputRepoOwner string
	OutputRepoName  string

	// GitHubWebhookSecret verifies the modules-repo webhook's
	// X-Hub-Signature-256 header (catalog.VerifySignature).
	GitHubWebhookSecret string

	// TrackedRef is the fully-qualified ref the webhook's push handler
	// reacts to, e.g. "refs/heads/main". Pushes to any other ref are
	// ignored per the edge-case table.
	TrackedRef string

	// ReconcileInterval is how often catalog.Service.RunPeriodically
	// re-scans the modules repo (Fallback 1 in the Catalog refresh locked
	// decision).
	ReconcileInterval time.Duration

	// WebhookDedupWindow is how long catalog.InMemorySeenDeliveries retains
	// delivery IDs for duplicate-webhook-delivery dedup.
	WebhookDedupWindow time.Duration

	// StateBackendType selects the Terraform backend block codegen writes
	// into backend.tf ("gcs" or "s3"), per the locked "State backend"
	// decision's STATE_BACKEND toggle.
	StateBackendType string

	// StateBackendBucket is the self-hosted state bucket name written into
	// every record's one-time backend.tf. The per-record "prefix" is
	// computed at commit time from the record's own output path -- it is
	// not part of static config.
	StateBackendBucket string

	// --- P3-B/P3-C: OIDC SSO (all optional -- see below) ---

	// OIDCIssuerURL is the IdP's OIDC issuer for discovery
	// (auth.OIDCConfig.IssuerURL). Left empty (the default -- SSO has no
	// required env vars), auth.NewOIDCProvider returns (nil, nil): SSO is
	// considered "not configured" and main.go serves password-only login,
	// per the migration-window design documented in
	// contracts/openapi.yaml. When set, OIDCClientID/OIDCClientSecret/
	// OIDCRedirectURL become required (validated below) since a
	// half-configured IdP integration should fail startup loudly rather
	// than silently misbehave at login time.
	OIDCIssuerURL string

	// OIDCClientID / OIDCClientSecret are this app's registration with the
	// IdP.
	OIDCClientID     string
	OIDCClientSecret string

	// OIDCRedirectURL is the callback URL registered with the IdP -- must
	// match this deployment's /auth/sso/callback absolute URL exactly.
	OIDCRedirectURL string

	// OIDCAllowedRedirectPrefixes allowlists frontend paths the
	// /auth/sso/login `redirect_to` query param may target (comma-
	// separated in the env var), per auth.OIDCConfig.AllowedRedirectPrefixes'
	// doc comment. Empty by default -- every redirect_to then falls back
	// to OIDCDefaultRedirectTo.
	OIDCAllowedRedirectPrefixes []string

	// OIDCDefaultRedirectTo is the frontend path landed on after a
	// successful SSO login when redirect_to is absent or not allowlisted.
	OIDCDefaultRedirectTo string
}

// usesGitHubApp reports whether GitHub App credentials (rather than a PAT)
// should be used to authenticate to GitHub.
func (c *Config) UsesGitHubApp() bool {
	return c.GitHubToken == "" && c.GitHubAppID != ""
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func requireEnv(missing *[]string, key string) string {
	v := os.Getenv(key)
	if v == "" {
		*missing = append(*missing, key)
	}
	return v
}

// Load reads and validates configuration from the process environment.
// Recognized variables:
//
//	HTTP_ADDR                        (default ":8080")
//	DATABASE_URL                     (required)
//	JWT_SIGNING_SECRET               (required)
//	GITHUB_TOKEN                     (PAT; either this or the GITHUB_APP_* trio is required)
//	GITHUB_APP_ID
//	GITHUB_APP_INSTALLATION_ID
//	GITHUB_APP_PRIVATE_KEY            (PEM contents; "\n" literal sequences are unescaped)
//	GITHUB_APP_PRIVATE_KEY_PATH       (alternative to the above: a file path)
//	MODULES_REPO_OWNER               (required)
//	MODULES_REPO_NAME                (required)
//	MODULES_PATH                     (default "modules")
//	OUTPUT_REPO_OWNER                (required)
//	OUTPUT_REPO_NAME                 (required)
//	GITHUB_WEBHOOK_SECRET             (required)
//	MODULES_REPO_TRACKED_REF          (default "refs/heads/main")
//	CATALOG_RECONCILE_INTERVAL        (Go duration string, default "1h")
//	WEBHOOK_DEDUP_WINDOW              (Go duration string, default "24h")
//	STATE_BACKEND                     ("gcs" or "s3", default "gcs")
//	STATE_BACKEND_BUCKET             (required)
//	OIDC_ISSUER_URL                   (optional; empty = SSO disabled)
//	OIDC_CLIENT_ID                    (required if OIDC_ISSUER_URL is set)
//	OIDC_CLIENT_SECRET                (required if OIDC_ISSUER_URL is set)
//	OIDC_REDIRECT_URL                 (required if OIDC_ISSUER_URL is set)
//	OIDC_ALLOWED_REDIRECT_PREFIXES    (comma-separated, optional)
//	OIDC_DEFAULT_REDIRECT_TO          (default "/")
func Load() (*Config, error) {
	var missing []string

	cfg := &Config{
		HTTPAddr:            getenv("HTTP_ADDR", ":8080"),
		DatabaseURL:         requireEnv(&missing, "DATABASE_URL"),
		ModulesRepoOwner:    requireEnv(&missing, "MODULES_REPO_OWNER"),
		ModulesRepoName:     requireEnv(&missing, "MODULES_REPO_NAME"),
		ModulesPath:         getenv("MODULES_PATH", "modules"),
		OutputRepoOwner:     requireEnv(&missing, "OUTPUT_REPO_OWNER"),
		OutputRepoName:      requireEnv(&missing, "OUTPUT_REPO_NAME"),
		GitHubWebhookSecret: requireEnv(&missing, "GITHUB_WEBHOOK_SECRET"),
		TrackedRef:          getenv("MODULES_REPO_TRACKED_REF", "refs/heads/main"),
		StateBackendType:    getenv("STATE_BACKEND", "gcs"),
		StateBackendBucket:  requireEnv(&missing, "STATE_BACKEND_BUCKET"),
	}

	secret := requireEnv(&missing, "JWT_SIGNING_SECRET")
	cfg.JWTSigningSecret = []byte(secret)

	cfg.GitHubToken = os.Getenv("GITHUB_TOKEN")
	if cfg.GitHubToken == "" {
		cfg.GitHubAppID = os.Getenv("GITHUB_APP_ID")
		cfg.GitHubAppInstallationID = os.Getenv("GITHUB_APP_INSTALLATION_ID")
		if cfg.GitHubAppID == "" || cfg.GitHubAppInstallationID == "" {
			missing = append(missing, "GITHUB_TOKEN (or GITHUB_APP_ID + GITHUB_APP_INSTALLATION_ID + a private key)")
		} else {
			pem, err := loadAppPrivateKey()
			if err != nil {
				return nil, err
			}
			cfg.GitHubAppPrivateKeyPEM = pem
		}
	}

	var err error
	cfg.ReconcileInterval, err = parseDurationEnv("CATALOG_RECONCILE_INTERVAL", time.Hour)
	if err != nil {
		return nil, err
	}
	cfg.WebhookDedupWindow, err = parseDurationEnv("WEBHOOK_DEDUP_WINDOW", 24*time.Hour)
	if err != nil {
		return nil, err
	}

	if cfg.StateBackendType != "gcs" && cfg.StateBackendType != "s3" {
		return nil, fmt.Errorf("config: STATE_BACKEND must be \"gcs\" or \"s3\", got %q", cfg.StateBackendType)
	}

	cfg.OIDCIssuerURL = os.Getenv("OIDC_ISSUER_URL")
	if cfg.OIDCIssuerURL != "" {
		// A half-configured OIDC integration should fail startup loudly
		// rather than silently misbehave the first time someone hits
		// /auth/sso/login -- unlike the trio above, these are only
		// required conditionally.
		cfg.OIDCClientID = requireEnv(&missing, "OIDC_CLIENT_ID")
		cfg.OIDCClientSecret = requireEnv(&missing, "OIDC_CLIENT_SECRET")
		cfg.OIDCRedirectURL = requireEnv(&missing, "OIDC_REDIRECT_URL")
	} else {
		cfg.OIDCClientID = os.Getenv("OIDC_CLIENT_ID")
		cfg.OIDCClientSecret = os.Getenv("OIDC_CLIENT_SECRET")
		cfg.OIDCRedirectURL = os.Getenv("OIDC_REDIRECT_URL")
	}
	cfg.OIDCDefaultRedirectTo = getenv("OIDC_DEFAULT_REDIRECT_TO", "/")
	if raw := os.Getenv("OIDC_ALLOWED_REDIRECT_PREFIXES"); raw != "" {
		for _, prefix := range strings.Split(raw, ",") {
			prefix = strings.TrimSpace(prefix)
			if prefix != "" {
				cfg.OIDCAllowedRedirectPrefixes = append(cfg.OIDCAllowedRedirectPrefixes, prefix)
			}
		}
	}

	if len(missing) > 0 {
		return nil, fmt.Errorf("config: missing required environment variable(s): %s", strings.Join(missing, ", "))
	}

	return cfg, nil
}

func loadAppPrivateKey() ([]byte, error) {
	if raw := os.Getenv("GITHUB_APP_PRIVATE_KEY"); raw != "" {
		// Many env-var mechanisms (docker-compose, some CI systems) can't
		// hold a real multi-line PEM cleanly; accept literal "\n" escapes.
		return []byte(strings.ReplaceAll(raw, `\n`, "\n")), nil
	}
	if path := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("config: reading GITHUB_APP_PRIVATE_KEY_PATH: %w", err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("config: GITHUB_APP_ID is set but neither GITHUB_APP_PRIVATE_KEY nor GITHUB_APP_PRIVATE_KEY_PATH is set")
}

func parseDurationEnv(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("config: invalid %s %q: %w", key, raw, err)
	}
	return d, nil
}

// Admin-bootstrap knobs are intentionally not part of Config: Phase 1 has
// no first-run admin-bootstrap requirement in the frozen API surface (users
// are expected to be seeded directly, e.g. via a migration or a one-off
// script) -- adding one here would be speculative.
