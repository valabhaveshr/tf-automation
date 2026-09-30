// Command seed-admin creates (or promotes) a single local-login user with
// the admin role, so there's a way to log in at all -- Phase 1 deliberately
// has no signup endpoint (see internal/config/config.go's bottom comment:
// "users are expected to be seeded directly, e.g. via a migration or a
// one-off script"). This is that script. Not part of any workstream's
// original scope; added to make local testing possible.
//
// Usage:
//
//	DATABASE_URL=postgres://... go run ./cmd/seed-admin -username admin -password change-me
//
// If the username already exists, its password is left untouched and it is
// simply granted the admin role (idempotent — safe to re-run).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"tf-automation/backend/internal/auth"
	"tf-automation/backend/internal/data"
)

func main() {
	username := flag.String("username", "", "login username (required)")
	password := flag.String("password", "", "login password (required, min 8 chars)")
	flag.Parse()

	if *username == "" || *password == "" {
		fmt.Fprintln(os.Stderr, "usage: seed-admin -username <name> -password <secret>")
		os.Exit(2)
	}
	if len(*password) < 8 {
		log.Fatal("password must be at least 8 characters")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL must be set")
	}

	ctx := context.Background()
	store, err := data.Connect(ctx, dbURL)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer store.Pool().Close()

	if existing, err := store.GetUserByUsername(ctx, *username); err == nil {
		if err := store.AssignRole(ctx, existing.ID, data.RoleAdmin); err != nil {
			log.Fatalf("granting admin role to existing user %q: %v", *username, err)
		}
		fmt.Printf("user %q already existed — granted admin role (password unchanged)\n", *username)
		return
	}

	hash, err := auth.HashPassword(*password)
	if err != nil {
		log.Fatalf("hashing password: %v", err)
	}

	u, err := store.CreateUser(ctx, &data.User{
		Username:     *username,
		PasswordHash: hash,
		AuthProvider: "local",
	})
	if err != nil {
		log.Fatalf("creating user: %v", err)
	}
	// CreateUser already grants "requester"; this test/admin account also
	// needs "admin" to exercise catalog refresh, naming templates,
	// customers, and the audit log.
	if err := store.AssignRole(ctx, u.ID, data.RoleAdmin); err != nil {
		log.Fatalf("granting admin role: %v", err)
	}

	fmt.Printf("created user %q (id=%s) with roles [requester admin]\n", *username, u.ID)
}
