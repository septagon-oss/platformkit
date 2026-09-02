package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// passwordEnv is where a bootstrap password comes from when somebody has one in
// mind. Command-line arguments are in the process table and in shell history,
// so a password is not a flag.
const passwordEnv = "PLATFORMKIT_BOOTSTRAP_PASSWORD"

// bootstrap creates the first tenant of an empty installation and the
// administrator who signs in to it.
//
// It refuses when any tenant already exists, and that refusal is what makes it
// safe to leave in the binary: this is the one write in the application with no
// caller to authorize, so the condition that protects it is that it can only
// ever happen once. Every tenant after the first is created through the API, by
// somebody holding tenant:manage.
//
// The whole thing is one transaction — migrations, the tenant, its roles, the
// administrator — so an installation is either usable or untouched.
func bootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	slug := fs.String("tenant", "", "Slug of the first tenant, a DNS label")
	host := fs.String("host", "", "Host the first tenant is served at")
	name := fs.String("name", "", "Display name of the first tenant")
	email := fs.String("admin-email", "", "Address of the first administrator")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct{ flag, value string }{
		{"--tenant", *slug}, {"--host", *host}, {"--name", *name}, {"--admin-email", *email},
	} {
		if required.value == "" {
			return fmt.Errorf("bootstrap: %s is required", required.flag)
		}
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	logger(cfg.Log.Level)

	password, generated := os.Getenv(passwordEnv), false
	if password == "" {
		if password, err = generatePassword(); err != nil {
			return err
		}
		generated = true
	}

	ctx := context.Background()
	c := compose(cfg)
	err = app.Bootstrap(ctx, cfg, func(ctx context.Context, tx db.Tx[db.System]) error {
		t, err := tenant.Bootstrap(ctx, tx, c.tenants, tenantcontracts.NewTenant{
			Slug: *slug, Name: *name, Host: *host,
		})
		if err != nil {
			return err
		}
		// The roles this administrator is about to be granted were seeded by
		// the hook inside Create, in this same transaction. See modules.go.
		_, err = c.users.Provision(ctx, tx, t.ID, *email, "", password,
			[]string{authcontracts.RoleAdmin})
		return err
	})
	if err != nil {
		return err
	}

	// stdout is the machine-readable half — a script pipes it — and the
	// password goes to stderr, once, because it is never stored anywhere it
	// could be read back: what is in the database is an argon2id hash.
	fmt.Printf("tenant %s at %s, administrator %s\n", *slug, *host, *email)
	if generated {
		fmt.Fprintf(os.Stderr, "\n  password for %s: %s\n  It is not stored and will not be shown again.\n\n", *email, password)
	}
	return nil
}

// generatePassword is 24 bytes of crypto/rand, base64url: 192 bits, which is
// past anything a length rule is about.
func generatePassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", errors.New("bootstrap: no randomness to generate a password with")
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
