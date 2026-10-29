package main

// The reference application's seed: the files under seed/, one writer per
// resource, and the grant check a seed run passes before it asks an owner for a
// write. Nothing here is a second write path. A page is created, patched and
// deleted through content's own Spec write core — the row lock, the tenant
// recheck and the event that the JSON route runs — and a person is created
// through the user module's own invitation. A seeded row is an ordinary row:
// validated by its owner, authored by whoever ran the command, and announced by
// the event its owner publishes, which is how it reaches the audit trail.

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// seedFiles is the application's own seed, embedded for the same reason its
// migrations are: a binary that cannot seed the tenant it creates is a binary
// that needs a source tree to finish its own job.
//
//go:embed all:seed
var seedFiles embed.FS

// seedClock is the seed's Clock. It reads the one clock every timestamp in this
// application comes from, so a relative date in a seed file resolves to the
// instant the database would have stored anyway.
type seedClock struct{}

func (seedClock) Now() time.Time { return db.Now() }

// seedService is the whole seed surface of this application: the literal writer
// list, the embedded files, and the authorizer. It is a function and not a field
// of composition because nothing reads it until a seed command asks for it.
// demoPassword is config's Demo.Password, which whoever runs a command already
// holds: the password a demonstration's people sign in with is a deployment's
// answer, and no file under seed/ carries one.
func seedService(c composition, demoPassword string) (*seed.Service, error) {
	return seed.New(seed.Deps{
		Files: seedFiles, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{
			&contentSeeder{svc: c.contents},
			&siteSeeder{sites: c.sites},
			&userSeeder{users: c.seedUsers, demoPassword: demoPassword},
			&taskSeeder{svc: c.tasks},
			&fileSeeder{svc: c.files},
		},
		Authorize: seedGrants{auth: c.permissions},
	})
}

// seeding contributes the tenant-creation hook that fills a new tenant with this
// application's starter content. It declares one thing and needs nothing, which is
// not an omission but the fact the resolver names when it is declared otherwise.
//
// The owners a seeded record is written through cannot be Needs of this module: task
// needs jobs.TenantLister, which tenant provides, and auth puts its authorizer behind
// tenant too, so needing either while contributing a hook tenant takes is the cycle
// the build refuses — seed → task → tenant → seed. The hook must exist before the
// tenant module is built, and the services it writes through are built after it.
//
// So the provisioner is the composition's one late-bound value, filled in app.go's
// composeReference from the resolved plan, beside the two values that file already
// fills the same way. OnTenantCreate refuses to write anything until then, so a
// composition that forgot the line fails by name rather than by a nil interface.
func seeding(provision *seedProvisioner) *pkit.Module {
	return pkit.NewModule("seed", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[tenantcontracts.Hook](w, tenantcontracts.Hook(provision.OnTenantCreate))
		return module.Module{Name: "seed"}, nil
	}, pkit.Contributes[tenantcontracts.Hook]())
}

// seedProvisioner is this application's second tenant-creation hook: the new
// tenant's own starter content, written before anybody could ask for it, because an
// app opens on something rather than on an empty site waiting for a command.
//
// The authority is not its to invent: it calls seed.Service.ApplyProvisioned, which
// refuses unless the tenant holds no seeded record, and it refuses here unless the
// tenant holds no person. Together those say "this tenant came into being in this
// transaction", the only claim a run with nobody to ask the auth module about is
// entitled to make. What it can write is then exactly what its own files declare and
// nobody has written yet, through the same owners and the same events a click makes.
//
// The fields are the owners it writes through, filled once by composeReference; see
// seeding for why no module can declare them as its needs.
type seedProvisioner struct {
	users        usercontracts.SeedWriter
	contents     contentcontracts.Service
	sites        sitecontracts.Service
	tasks        taskcontracts.Service
	files        filecontracts.Service
	auth         authcontracts.PermissionReader
	demoPassword string
}

func (p *seedProvisioner) OnTenantCreate(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
	if p.users == nil || p.contents == nil || p.sites == nil || p.tasks == nil || p.files == nil || p.auth == nil {
		return errors.New("seed: the creation hook ran before the composition filled its owners")
	}
	service, err := seed.New(seed.Deps{
		Files: seedFiles, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{
			&contentSeeder{svc: p.contents},
			&siteSeeder{sites: p.sites},
			&userSeeder{users: p.users, demoPassword: p.demoPassword},
			&taskSeeder{svc: p.tasks},
			&fileSeeder{svc: p.files},
		},
		// The same authorizer a command run goes through, with one branch:
		// seedGrants lets a provisioning run through where it would ask a person.
		Authorize: seedGrants{auth: p.auth},
	})
	if err != nil {
		return err
	}
	// The create transaction's own trace if it has one — a control-plane
	// request, a bootstrap — and a trace minted here if it does not, so the
	// events this hook causes can always be joined to one another.
	if _, ok := trace.From(ctx); !ok {
		ctx = trace.With(ctx, trace.New())
	}
	return db.InTenant(ctx, tx, t.Tenancy(), func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		// The state check ApplyProvisioned cannot make about a table it does not
		// own: a tenant with anybody in it has been used, and a run with no
		// person in it has no business writing its content.
		if _, people, err := crud.List[*usercontracts.User](tx, crud.Query{Limit: 1}); err != nil {
			return err
		} else if people > 0 {
			return fmt.Errorf("seed: tenant %s already has people, so it is not being provisioned", t.Slug)
		}
		// The tenant row decides demo, exactly as it does for a command run: the
		// hook reads the record that was just inserted, not a caller's wish.
		ctx, err := events.ProvisionOperation(ctx, tx, "seed.provision", db.Now)
		if err != nil {
			return err
		}
		if _, err := service.ApplyProvisioned(ctx, tx, seed.Selection{Demo: t.Demo}); err != nil {
			return err
		}
		return events.Succeed(ctx, tx, db.Now())
	})
}

// seedGrants is the grant check a seed run passes for every record it is about
// to write: the roles the person it writes as holds in this transaction right
// now, the permissions those roles hold, and the resource's own permission
// compared between them — the same three reads kit/httpx makes of a request, in
// the seed's own transaction.
//
// It refuses a run that carries no person rather than assuming one. That is the
// point of it: seeding is not its own authority. Whoever runs it holds, through
// roles the tenant's own rows name, the permission each write needs, and a
// composition that could seed without naming one would be a composition that
// could seed anything. What provisioning a brand-new tenant needs instead — a
// permit minted by the create transaction itself — is named in
// kit/seed/README.md's Limits and is not composed here.
type seedGrants struct {
	auth authcontracts.PermissionReader
}

func (g seedGrants) Check(ctx context.Context, tx db.Tx[db.Tenant], r seed.Resource, _ seed.Action) error {
	// The one run with nobody to ask is the tenant's own creation, and the
	// conditions that make it safe are state, not a claim: see
	// seed.Service.ApplyProvisioned (the tenant holds no seeded record and every
	// record the files name is still absent) and seedProvisioner below (the tenant
	// holds no person). Together they mean this run can only give a tenant that
	// does not exist yet the records every tenant of this application is created
	// with. Anything else names a person — and the branch is taken before the
	// person is read again below, because the caller a create request happened to
	// authenticate is not a person of a tenant that has no people in it yet.
	if seed.Provisioning(ctx) {
		return nil
	}
	caller, ok := tenancy.PrincipalFrom(ctx)
	if !ok || len(caller.Roles) == 0 {
		return fmt.Errorf("seed: %s/%s: this run carries no person and no roles", r.Module, r.Entity)
	}
	// The roles on the context were read when the run resolved --as, and a snapshot
	// is not an authority. A transaction that took this person's roles after that
	// read has committed by the time a run that waited comes back to write, and the
	// row this run is about to author would be its own. So the question is put to
	// what the tenant's rows say now, in this transaction — the same person's roles
	// and status, re-read, the same permission compared against them — and not to
	// what the context remembers them being.
	actor, err := crud.Get[*usercontracts.User](tx, caller.UserID)
	if err != nil {
		return fmt.Errorf("seed: %s/%s: the person this run writes as cannot be asked again: %w", r.Module, r.Entity, err)
	}
	if actor.Status != usercontracts.StatusActive {
		return fmt.Errorf("seed: %s/%s: this run writes as %s, who is %s and holds nothing here", r.Module, r.Entity, actor.Email, actor.Status)
	}
	if len(actor.Roles) == 0 {
		return fmt.Errorf("seed: %s/%s: this run writes as %s, who now holds no roles", r.Module, r.Entity, actor.Email)
	}
	held, err := g.auth.Permissions(ctx, tx, actor.Roles)
	if err != nil {
		return err
	}
	if !authcontracts.Grants(held, tenancy.Grant{Permission: r.WriteGrant}) {
		return fmt.Errorf("grant %s is not held", r.WriteGrant)
	}
	return nil
}

// seedActor is the person a seed run writes as. The user module reads the row, in
// the same tenant transaction the writes will happen in; the principal carries the
// roles that row holds, read from the row rather than from a credential, because
// the only credential an operator command has is the database it is pointed at.
// What goes on the context is the principal and not the actor, for the reason
// spelled out where the two are set below: a seeded row says who ran it, and does
// not claim somebody signed in to write it.
//
// The person must be an active one. A deactivated row keeps its hash and its
// roles, and a run that read the roles anyway would authorise a write through a
// person the tenant has switched off — the same reason kit/httpx refuses a
// session whose user is not active, on the side of the run that names who the
// writes belong to rather than the side that proves who is standing at the
// terminal (see seedOperator).
func seedActor(ctx context.Context, users usercontracts.AddressLookup, tx db.Tx[db.Tenant], email string) (context.Context, error) {
	person, err := users.ByEmail(ctx, tx, email)
	if err != nil {
		return ctx, fmt.Errorf("seed: this run writes as %s, who is not a person of this tenant: %w", email, err)
	}
	if person.Status != usercontracts.StatusActive {
		return ctx, fmt.Errorf("seed: this run writes as %s, who is %s and not an active person of this tenant, so this run names nobody", email, person.Status)
	}
	// The principal, and deliberately not the actor. tenancy.Actor means "the
	// person whose session this is", and no session wrote a seeded row: the run
	// did, on that person's behalf. The outbox column therefore stays NULL —
	// migrations/000052_outbox_attribution.up.sql says why — and the person is
	// named beside it as the run's initiator, taken off this principal by
	// kit/seed.
	return tenancy.WithPrincipal(ctx, tenancy.Principal{
		UserID: person.ID, Roles: []string(person.Roles),
	}), nil
}

// seedRoles_ reads the record's roles field as the canonical list both sides of
// the comparison use. YAML hands a list as []any of strings; the module hands
// contracts.Roles. Both arrive here and leave as one thing, because Decide
// compares Go types and not intentions.
func seedRoles_(v any) ([]string, error) {
	switch v := v.(type) {
	case nil:
		return []string{}, nil
	case []string:
		return seedRolesSorted(v), nil
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			name, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("roles holds %v, which is not a role name", item)
			}
			out = append(out, name)
		}
		return seedRolesSorted(out), nil
	}
	return nil, fmt.Errorf("roles holds %v, which is not a list of role names", v)
}

// seedRolesSorted is a role set in the one spelling both sides of the comparison
// use — the module's own, contracts.CanonicalRoles, which is what SetRoles stores.
// Trimmed, lower-cased, deduplicated and sorted, and the empty answer collapsed to
// one value: an invited person holds no roles, a file that names none asks for
// none, and those two sentences agree only if both sides print the same empty
// slice. Comparing against a spelling the owner would never store is a comparison
// that ends in a write on every run forever, for a file that never changed.
func seedRolesSorted(roles []string) []string {
	return []string(usercontracts.CanonicalRoles(roles))
}

// seedTexts reads the fields a writer takes from a record as the text its owners
// hold, and refuses the first value that is not text.
//
// What the file left out and what it wrote as something else are different facts: a
// name appears in the map only when the record wrote one, so `kind:` and no `kind` at
// all answer "nobody answered" while `kind: ""` answers the empty text that was
// written. Only the middle case is an absent field, and that is the only thing a
// writer's default answers — defaulting on top of the number 7 commits an answer
// nobody gave, and the next run compares its own default and calls it unchanged. So
// the value refuses, at the record's own line, as kit/seed does one level up.
//
// Text is carried, not judged, and trimmed, because the owners trim: a target that
// kept the spaces would differ from the stored row on every run. `priority: whenever`
// reaches task's own validation and comes back as task's own sentence.
func seedTexts(r seed.Record, names ...string) (map[string]string, error) {
	text := make(map[string]string, len(names))
	for _, name := range names {
		value, declared := r.Fields[name]
		if !declared || value == nil {
			continue // YAML's no value is a field the file left out
		}
		sentence, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s is written as %s, not as text: a value the record declares is never the field it left out, which is the only thing a default answers",
				name, seedShape(value))
		}
		text[name] = strings.TrimSpace(sentence)
	}
	return text, nil
}

// seedShape says what a value that is not text is, in the words a person would use
// about the file: `the number 7`, not `7 (int)` — the first thing they have to put
// back is the type they typed.
func seedShape(value any) string {
	switch v := value.(type) {
	case bool:
		return fmt.Sprintf("the boolean %t", v)
	case int, int64, uint64, float64:
		return fmt.Sprintf("the number %v", v)
	case time.Time:
		return fmt.Sprintf("the date %s", v.Format(time.RFC3339Nano))
	case []any:
		return "a list"
	case map[string]any:
		return "a mapping"
	default:
		return fmt.Sprintf("a %T", v)
	}
}

// seedTime is a create-only date read the way its owner's field is stored: a
// pointer, with anything else read as no date at all rather than as a zero one.
// A date only ever arrives here from a writer that resolved it in Target, so
// anything but a time.Time is a writer that put the wrong thing in the map — and
// "no deadline" is the answer a wrong value earns, not a midnight nobody asked
// for.
func seedTime(v any) *time.Time {
	t, ok := v.(time.Time)
	if !ok {
		return nil
	}
	return &t
}

// seedCommands names the commands a record asks for out of the ones its writer
// offers. Anything else in the file is refused earlier, by the service.
func seedCommands(r seed.Record, names ...string) map[string]any {
	out := map[string]any{}
	for _, c := range r.Commands {
		if slices.Contains(names, c.Name) {
			out[c.Name] = true
		}
	}
	return out
}
