// Command server is the tf-automation Phase 1 backend entrypoint (WS-G's
// consolidation point): it loads config, constructs every workstream's real
// (non-mock) implementation, wires them into the WS-G API router, starts
// the catalog's periodic reconciliation loop, and serves HTTP.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/go-github/v63/github"

	"tf-automation/backend/internal/api"
	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/catalog"
	"tf-automation/backend/internal/config"
	"tf-automation/backend/internal/data"
	"tf-automation/backend/internal/naming"
	"tf-automation/backend/internal/output"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- WS-A: Data layer ----
	store, err := data.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()

	// ---- GitHub auth wiring (this workstream's job; neither WS-D nor
	// WS-E were scoped to acquire credentials themselves) ----
	rt, err := buildGitHubTransport(cfg)
	if err != nil {
		return err
	}
	httpClient := &http.Client{Transport: rt, Timeout: 30 * time.Second}
	ghClient := github.NewClient(httpClient)

	// ---- WS-D: Catalog service + webhook ----
	modulesSource := catalog.NewGitHubSource(ghClient, cfg.ModulesRepoOwner, cfg.ModulesRepoName)
	catalogService := catalog.NewService(modulesSource, store, cfg.ModulesPath)
	webhookHandler := catalog.NewWebhookHandler(
		catalogService,
		cfg.GitHubWebhookSecret,
		cfg.TrackedRef,
		catalog.NewInMemorySeenDeliveries(cfg.WebhookDedupWindow),
		log.Default(),
	)

	// ---- WS-I: Naming Convention Engine ----
	namingEngine := naming.NewEngine(store)

	// ---- WS-E: Output repo integration ----
	outputClient := output.NewClient(ghClient)

	// ---- WS-F: Auth/sessions ----
	sessions := auth.New(cfg.JWTSigningSecret)

	// ---- P3-B/P3-C: OIDC SSO (optional; nil when OIDC_ISSUER_URL is
	// unset, per auth.NewOIDCProvider's doc comment) ----
	ssoProvider, err := auth.NewOIDCProvider(ctx, auth.OIDCConfig{
		IssuerURL:               cfg.OIDCIssuerURL,
		ClientID:                cfg.OIDCClientID,
		ClientSecret:            cfg.OIDCClientSecret,
		RedirectURL:             cfg.OIDCRedirectURL,
		DefaultRedirectTo:       cfg.OIDCDefaultRedirectTo,
		AllowedRedirectPrefixes: cfg.OIDCAllowedRedirectPrefixes,
	})
	if err != nil {
		return err
	}
	if ssoProvider != nil {
		ssoProvider.Sessions = sessions
		ssoProvider.Users = api.NewSSOUserUpserter(store)
	}

	// ---- WS-G: API router ----
	srv := &api.Server{
		Store:              store,
		Catalog:            catalogService,
		Webhook:            webhookHandler,
		Naming:             namingEngine,
		Output:             outputClient,
		Sessions:           sessions,
		SSO:                ssoProvider,
		OutputRepoOwner:    cfg.OutputRepoOwner,
		OutputRepoName:     cfg.OutputRepoName,
		ModulesRepoOwner:   cfg.ModulesRepoOwner,
		StateBackendType:   cfg.StateBackendType,
		StateBackendBucket: cfg.StateBackendBucket,
	}

	// Fallback 1 of the Catalog refresh locked decision: periodic
	// reconciliation, catching webhook deliveries GitHub failed to send or
	// that failed to process.
	go catalogService.RunPeriodically(ctx, cfg.ReconcileInterval)

	httpServer := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      srv.Routes(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second, // generous: record save fans out to multiple sequential GitHub API calls
		IdleTimeout:  120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server: listening on %s", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Printf("server: shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}
