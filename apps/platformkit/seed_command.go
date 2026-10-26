package main

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// seedCommand applies this application's embedded seed to one tenant.
//
// Three things carry the authority story, and only two are flags. `--tenant` names
// which tenant: its own row decides whether demo records are its own, so an operator
// cannot widen a run by asking. `--as` names the person the writes are attributed to,
// and the seed's authorizer asks auth, for every record, whether that person's roles
// hold each resource's permission. Neither says who is at the terminal — naming an
// address is not proving it — so the third is a credential, seed.operator_email and
// seed.operator_password, asked of the installation tenant's own people, of a role
// that names the operator grant, before this command enters its tenant. Missing one
// and the run refuses: no mode seeds as nobody.
//
// `--dry-run` writes nothing and prints what a real run would, the decision being the
// same code either way; the plan prints only after the transaction commits.
func seedCommand(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	path := fs.String("config", "config.yaml", "Path to the configuration file")
	slug := fs.String("tenant", "", "Slug of the tenant to seed")
	as := fs.String("as", "", "Address of the person this run writes as")
	demo := fs.Bool("demo", false, "Include the demo records, for a tenant whose own row says it is a demo one")
	dry := fs.Bool("dry-run", false, "Print what this run would write and write nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, required := range []struct{ flag, value string }{
		{"--tenant", *slug}, {"--as", *as},
	} {
		if required.value == "" {
			return fmt.Errorf("seed: %s is required", required.flag)
		}
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	// Asked before a connection is opened: a run that cannot say who is running
	// it has no business touching a database at all, and the refusal should not
	// depend on whether one is listening.
	if cfg.Seed.OperatorEmail == "" || cfg.Seed.OperatorPassword == "" {
		return errors.New("seed: an operator credential is required: set " +
			"PLATFORMKIT_SEED_OPERATOR_EMAIL and PLATFORMKIT_SEED_OPERATOR_PASSWORD; --as names who a write belongs to, not who is running it")
	}
	logger(cfg.Log.Level)
	c := compose(cfg)
	service, err := seedService(c, cfg.Demo.Password)
	if err != nil {
		return err
	}

	var plan seed.Plan
	var minted []mintedCredential
	err = app.RunCommand(context.Background(), cfg, func(ctx context.Context, system db.Tx[db.System]) error {
		// Whatever passwords this run mints land in `minted` and are printed below,
		// after the commit: a command line has somebody reading it, a create
		// transaction has nobody, and neither of the two is a log.
		ctx = withMinted(ctx, &minted)
		// One trace for the run, minted here because a command line inherits no
		// request to inherit one from. Every owner event the run causes carries
		// it into the outbox (kit/events reads it there), which is what lets a
		// later reader join twenty rows of writes and one plan print-out back to
		// one invocation. docs/seed.md's audit contract asks for the same id.
		if _, ok := trace.From(ctx); !ok {
			ctx = trace.With(ctx, trace.New())
		}
		target, err := seedTenant(ctx, c.tenantList, system, *slug)
		if err != nil {
			return err
		}
		// The operator is proven in the installation tenant's own scope, in this
		// same transaction, before the target tenant is entered.
		if err := seedOperator(ctx, c, system, cfg); err != nil {
			return err
		}
		return db.InTenant(ctx, system, target, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.addresses, tx, *as)
			if err != nil {
				return err
			}
			selection := seed.Selection{Demo: *demo}
			if *dry {
				plan, err = service.Plan(ctx, tx, selection)
			} else {
				ctx, err = app.AdmitCommand(ctx, tx, "seed.apply", db.Now)
				if err != nil {
					return err
				}
				plan, err = service.Apply(ctx, tx, selection)
			}
			return err
		})
	})
	if err != nil {
		return err
	}
	fmt.Print(plan.String())
	// After the plan, and after the commit: a credential printed for a run that
	// then rolled back names a person who does not exist. This is the only place
	// a minted password reaches an output stream, and the only run that reaches it
	// is the one with somebody at the other end of it — see mintedCredential.
	printMintedCredentials(minted)
	return nil
}

func seedTenant(ctx context.Context, tenants tenantcontracts.Lister, tx db.Tx[db.System], slug string) (tenancy.Tenant, error) {
	all, err := tenants.List(ctx, tx)
	if err != nil {
		return tenancy.Tenant{}, err
	}
	for _, tenant := range all {
		if tenant.Slug == slug {
			return tenant.Tenancy(), nil
		}
	}
	return tenancy.Tenant{}, fmt.Errorf("seed: no tenant %s in this installation", slug)
}

// seedOperator asks the installation tenant's own user and role services three
// questions: is the person behind the credential who they say they are, can that
// person still sign in, and does that person hold tenant:manage where the
// installation lives. All three are reads of rows, in the transaction the command
// already opened, through the modules that own those rows.
//
// tenant:manage is the permission the command is actually exercising: it acts on a
// tenant other than the operator's own, and a tenant's administrator is not by that
// fact an operator of the installation. The check is therefore made in the
// installation tenant's scope: `--as` answers whose rows the writes name, and nothing
// answers "who asked" but the credential.
//
// A refusal says no more than that, and nothing here echoes a credential.
func seedOperator(ctx context.Context, c composition, system db.Tx[db.System], cfg config.Config) error {
	if cfg.Server.InstallationHost == "" {
		return errors.New("seed: server.installation_host names no tenant to authenticate an operator against")
	}
	installation, err := c.hostLookup.ByHost(ctx, system, cfg.Server.InstallationHost)
	if err != nil {
		return fmt.Errorf("seed: no installation tenant at %s to authenticate an operator at: %w", cfg.Server.InstallationHost, err)
	}
	return db.InTenant(ctx, system, installation, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := c.addresses.ByEmail(ctx, tx, cfg.Seed.OperatorEmail)
		if err != nil {
			return fmt.Errorf("seed: the operator %s is not a person of the installation tenant: %w", cfg.Seed.OperatorEmail, err)
		}
		if !person.CheckPassword(cfg.Seed.OperatorPassword) {
			return errors.New("seed: the operator credential matches nobody: this run writes nothing")
		}
		// Asked after the password, so a wrong credential never reports that the
		// address it names has been switched off, and asked at all because a hash
		// and a set of roles survive a deactivation: the row keeps everything that
		// would make its old password a seed grant. CanSignIn is the user module's
		// own answer, and the one kit/httpx asks of a session — which is the
		// promise this function's comment makes, and a promise a deactivated
		// operator's credential cannot keep.
		if !person.CanSignIn() {
			return fmt.Errorf("seed: %s is not an active person of the installation tenant, so this run names no operator", cfg.Seed.OperatorEmail)
		}
		held, err := c.permissions.Permissions(ctx, tx, []string(person.Roles))
		if err != nil {
			return err
		}
		if !authcontracts.Grants(held, tenancy.Grant{Permission: tenantcontracts.PermissionTenantManage, Operator: true}) {
			return fmt.Errorf("seed: %s holds no %s at the installation tenant, so this run names no operator",
				cfg.Seed.OperatorEmail, tenantcontracts.PermissionTenantManage)
		}
		return nil
	})
}
