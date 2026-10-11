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
// Three things carry the whole authority story, and only two of them are flags.
// `--tenant` names which tenant, and nothing else about it: the tenant's own row
// is what decides whether demo records are its own, so an operator cannot widen
// a run by asking. `--as` names the person the writes are attributed to — an
// address the user module resolves inside that tenant's transaction — and the
// seed's authorizer asks the auth module whether that person's roles hold each
// resource's permission, for every record. Neither of those says who is standing
// at the terminal: naming an address is not proving it, and the database account
// a shell can read is not a seed grant. So the third thing is a credential,
// `seed.operator_email` and `seed.operator_password` (PLATFORMKIT_SEED_OPERATOR_
// EMAIL and _PASSWORD), checked against the installation tenant's own people and
// their roles before this command enters the tenant it was pointed at.
//
// A run without one of the two flags refuses, and so does one without the
// credential: there is no mode in which the command seeds as nobody.
//
// `--dry-run` prints the plan and writes nothing, and prints the same text a run
// that writes would print: the decision is the same code either way, so a dry run
// that lied would be caught by the run it predicted. In both cases the plan is
// printed only after the transaction has committed, because a plan printed and
// then rolled back is a claim about rows that do not exist.
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
		tenants, err := c.tenants.List(ctx, system)
		if err != nil {
			return err
		}
		var target *tenancy.Tenant
		for _, t := range tenants {
			if t.Slug == *slug {
				found := t.Tenancy()
				target = &found
				break
			}
		}
		if target == nil {
			return fmt.Errorf("seed: no tenant %s in this installation", *slug)
		}
		// The operator is proven in the installation tenant's own scope, in this
		// same transaction, before the target tenant is entered.
		if err := seedOperator(ctx, c, system, cfg); err != nil {
			return err
		}
		return db.InTenant(ctx, system, *target, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.users, tx, *as)
			if err != nil {
				return err
			}
			selection := seed.Selection{Demo: *demo}
			if *dry {
				plan, err = service.Plan(ctx, tx, selection)
			} else {
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

// seedOperator asks the installation tenant's own user and role services three
// questions: is the person behind the credential who they say they are, can that
// person still sign in, and does that person hold tenant:manage where the
// installation lives. All three are reads of rows, in the transaction the command
// already opened, through the modules that own those rows — the same three reads
// kit/httpx makes of a request, with the request replaced by a credential no shell
// history holds.
//
// tenant:manage is the permission the command is actually exercising: it acts on
// a tenant other than the operator's own. A tenant's administrator is not by
// that fact an operator of the installation, which is why the check is made in
// the installation tenant's scope and not in the target's: `--as` answers "whose
// rows do these writes name", and nothing answers "who asked" but the credential.
//
// A refusal says no more than that. A wrong password is not reported as a wrong
// password for the named address, and nothing here prints or echoes one.
func seedOperator(ctx context.Context, c composition, system db.Tx[db.System], cfg config.Config) error {
	if cfg.Server.InstallationHost == "" {
		return errors.New("seed: server.installation_host names no tenant to authenticate an operator against")
	}
	installation, err := c.tenants.ByHost(ctx, system, cfg.Server.InstallationHost)
	if err != nil {
		return fmt.Errorf("seed: no installation tenant at %s to authenticate an operator at: %w", cfg.Server.InstallationHost, err)
	}
	return db.InTenant(ctx, system, installation, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := c.users.ByEmail(ctx, tx, cfg.Seed.OperatorEmail)
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
		held, err := c.auth.Permissions(ctx, tx, []string(person.Roles))
		if err != nil {
			return err
		}
		if !authcontracts.Grants(held, tenancy.Grant{Permission: tenantcontracts.PermissionTenantManage}) {
			return fmt.Errorf("seed: %s holds no %s at the installation tenant, so this run names no operator",
				cfg.Seed.OperatorEmail, tenantcontracts.PermissionTenantManage)
		}
		return nil
	})
}
