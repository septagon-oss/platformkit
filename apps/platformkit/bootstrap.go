package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

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
// administrator — so an installation is either usable or untouched. The one
// secret it takes arrives through config.Bootstrap, never a flag.
func bootstrap(args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	slug := fs.String("tenant", "", "Slug of the first tenant, a DNS label")
	host := fs.String("host", "", "Host the first tenant is served at")
	name := fs.String("name", "", "Display name of the first tenant")
	email := fs.String("admin-email", "", "Address of the first administrator")
	var spoken languagesFlag
	fs.Var(&spoken, "language", "A language this tenant's people are served in, repeatable; "+
		"the language the installation's copy is written in is always one of them")
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

	// The password comes through kit/config like every other secret:
	// bootstrap.password, which is PLATFORMKIT_BOOTSTRAP_PASSWORD in the
	// environment. Command-line arguments are in the process table and in
	// shell history, so it is not a flag. Empty means generate one.
	password, generated := cfg.Bootstrap.Password, false
	if password == "" {
		if password, err = generatePassword(); err != nil {
			return err
		}
		generated = true
	}

	ctx := context.Background()
	c := compose(cfg)
	err = app.Bootstrap(ctx, cfg, c.modules, func(ctx context.Context, tx db.Tx[db.System]) error {
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
		if err != nil {
			return err
		}
		// Which languages the first tenant is served in is a deployment's own
		// answer, and this is where it can say it: the module's create writes the
		// one language its copy is written in and nothing else, because a tenant's
		// set is a declaration and a create carries none. Said here rather than
		// written to the table afterwards, so an installation is still whole or
		// untouched, and through the module's own command rather than a second
		// INSERT, so the set is the one the installation has copy for.
		if len(spoken) > 0 {
			_, err = c.tenants.SetLocale(ctx, tx, t.ID, tenantcontracts.SetLocale{
				Default: t.DefaultLocale, Supported: spoken,
			})
		}
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

// languagesFlag is a repeatable --language: the flag package appends through Set
// rather than overwriting, which is how a list arrives on a command line.
type languagesFlag []string

func (l *languagesFlag) String() string { return strings.Join(*l, ",") }

func (l *languagesFlag) Set(tag string) error {
	*l = append(*l, tag)
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
