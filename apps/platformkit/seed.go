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
	"mime"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
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
func seedService(c composition) (*seed.Service, error) {
	return seed.New(seed.Deps{
		Files: seedFiles, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{
			&contentSeeder{svc: c.contents},
			&siteSeeder{sites: c.sites},
			&userSeeder{users: c.users, demoPassword: c.demoPassword},
			&taskSeeder{svc: c.tasks},
			&fileSeeder{svc: c.files},
		},
		Authorize: seedGrants{auth: c.auth},
	})
}

// seedProvisioner is this application's second tenant-creation hook: the new
// tenant's own starter content, written before anybody could ask for it.
//
// The reason it exists is the sentence in the brief — an app opens on something,
// not on an empty site waiting for a command. The reason it is short is that the
// authority is not its to invent: it calls seed.Service.ApplyProvisioned, which
// refuses unless the tenant holds no seeded record, and it refuses here unless
// the tenant holds no person. Together those two conditions say "this tenant
// came into being in this transaction", which is the only claim a run with
// nobody to ask the auth module about is entitled to make. What it can write is
// then exactly the records its own files declare and nobody has written yet,
// through the same owners, the same grants of the same module, and the same
// events a person's click produces.
//
// The fields are filled at the end of compose, in the same shape roleGranter
// takes: the hook has to exist before the tenant module does, and the owners it
// writes through are composed after it.
type seedProvisioner struct {
	users        usercontracts.Service
	contents     contentcontracts.Service
	sites        sitecontracts.Service
	tasks        taskcontracts.Service
	files        filecontracts.Service
	auth         authcontracts.Auth
	demoPassword string
}

func (p *seedProvisioner) OnTenantCreate(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
	if p.users == nil || p.contents == nil || p.sites == nil || p.tasks == nil || p.files == nil || p.auth == nil {
		return errors.New("seed: the creation hook ran before compose filled its owners")
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
		_, err := service.ApplyProvisioned(ctx, tx, seed.Selection{Demo: t.Demo})
		return err
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
type seedGrants struct{ auth authcontracts.Auth }

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
func seedActor(ctx context.Context, users usercontracts.Service, tx db.Tx[db.Tenant], email string) (context.Context, error) {
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
	// migrations/000037 says why — and the person is named beside it as the
	// run's initiator, taken off this principal by kit/seed.
	return tenancy.WithPrincipal(ctx, tenancy.Principal{
		UserID: person.ID, Roles: []string(person.Roles),
	}), nil
}

// contentSeeder seeds pages and posts. Slug is the natural key the file names a
// record by, so the seed finds a page the way a person does: by its address. An
// address is the owner's to spell, though — contracts.Slugify is what the write
// path runs and what the public read path looks up by — so this writer asks for
// the record's key the same way, and meets the row where the module put it. A
// seed that compared `About The Team` to the `about-the-team` it had just written
// would be a seed that patched the same page on every run, publishing an event
// nobody asked for, forever.
type contentSeeder struct{ svc contentcontracts.Service }

func (contentSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "contents", Module: "content", Entity: "content",
		NaturalKey: "slug", WriteGrant: contentcontracts.PermissionContentManage,
		// The slug is how the file addresses the record and how the module stores
		// it, so provenance, prune and reference lookup spell it the module's way.
		CanonicalKey: contentcontracts.Slugify,
		// Content has a delete path — the same soft delete the route uses — so a
		// file may prune records it declares gone.
		Prunable: true,
		Commands: []string{"publish"},
	}
}

func (contentSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	kind := seedText(r.Fields["kind"])
	if kind == "" {
		kind = contentcontracts.KindPage
	}
	if kind != contentcontracts.KindPage && kind != contentcontracts.KindPost {
		return seed.Target{}, fmt.Errorf("kind %q is not %s or %s", kind, contentcontracts.KindPage, contentcontracts.KindPost)
	}
	return seed.Target{
		Fields:   map[string]any{"slug": contentcontracts.Slugify(r.Key), "title": seedText(r.Fields["title"]), "body": seedText(r.Fields["body"]), "kind": kind},
		Commands: seedCommands(r, "publish"),
	}, nil
}

func (w *contentSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, forUpdate bool) (seed.Snapshot, error) {
	id := key.RecordID
	if id == uuid.Nil {
		// Provenance has no id, so this is the natural-key lookup. A seed run may
		// meet a manually created page with the same slug; the service decides
		// whether that row is its own to touch, and this only says what is there.
		rows, _, err := crud.List[*contentcontracts.Content](tx, crud.Query{Limit: 2, Filter: map[string]any{"slug": contentcontracts.Slugify(key.Value)}})
		if err != nil {
			return seed.Snapshot{}, err
		}
		if len(rows) == 0 {
			return seed.Snapshot{}, nil
		}
		id = rows[0].ID
	}
	var row *contentcontracts.Content
	var err error
	if forUpdate {
		row, err = crud.GetForUpdate[*contentcontracts.Content](tx, id)
	} else {
		row, err = crud.Get[*contentcontracts.Content](tx, id)
	}
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seeded(row), nil
}

func (w *contentSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	row, err := content.Spec.CreateRow(ctx, tx, &contentcontracts.Content{
		Slug: seedText(t.Fields["slug"]), Title: seedText(t.Fields["title"]),
		Body: seedText(t.Fields["body"]), Kind: seedText(t.Fields["kind"]),
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, seeded(row), t)
}

func (w *contentSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	// The slug is how the record was found, so it is not in the patch: a run that
	// renamed a page would be a run that deleted one page and created another.
	row, err := content.Spec.UpdateRow(ctx, tx, cur.ID, map[string]any{
		"title": t.Fields["title"], "body": t.Fields["body"], "kind": t.Fields["kind"],
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, seeded(row), t)
}

func (w *contentSeeder) Delete(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot) error {
	_, err := content.Spec.DeleteRow(ctx, tx, cur.ID)
	return err
}

// commands runs what the record declares after the row itself exists. Publish is
// the module's own command, so the lifecycle moves through the transition a
// person uses and announces itself as content.published rather than as a status
// some writer set from outside.
func (w *contentSeeder) commands(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	if want, _ := t.Commands["publish"].(bool); !want {
		return cur, nil
	}
	row, err := w.svc.Publish(ctx, tx, cur.ID)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seeded(row), nil
}

// seeded is content's canonical seed state: the four fields a file declares, and
// the lifecycle fact the publish command answers. Title, body and kind come back
// as the module stored them, which is the point — a normalised slug or a trimmed
// title compares equal on the next run because both sides read the stored value.
func seeded(row *contentcontracts.Content) seed.Snapshot {
	return seed.Snapshot{
		Present: true, ID: row.ID,
		Fields:   map[string]any{"slug": row.Slug, "title": row.Title, "body": row.Body, "kind": row.Kind},
		Commands: map[string]any{"publish": row.Status == contentcontracts.StatusPublished},
	}
}

// siteSeeder sets which page a tenant's public site opens on. It is a writer
// because the starter's home page is only "the home page" once the site says so:
// a seeded, published row that nothing points at leaves a visitor at "/" reading
// an empty site, which is the half of "your app opens on something" the content
// files cannot deliver on their own.
//
// One record per tenant, keyed by the word the file uses for it, because a site
// is a settings row and not a list. What the file declares is the home slug and
// nothing else: the title, tagline and theme of a site are a person's answers,
// and a seed that rewrote them on every deploy would be a seed that undid them.
type siteSeeder struct{ sites sitecontracts.Service }

func (siteSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "sites", Module: "site", Entity: "site",
		NaturalKey: "settings", WriteGrant: sitecontracts.PermissionSiteManage,
		// The home page is a reference to content, said as one: the seed orderer
		// resolves it, so the page is written before the site points at it, and a
		// target that exists in neither the files nor this tenant refuses at the
		// file's own line. Declaring nothing here would leave site.Save — which
		// checks that a slug is a slug, not that a page wears it — accepting a
		// home page that has never existed, and a visitor at "/" reading a
		// settings row about a row nobody can write.
		References: []seed.Reference{{Resource: "sites", Path: "fields/homeSlug", Target: "contents"}},
		// No delete path: the module offers no way to un-have a site, and
		// blanking a tenant's home slug is a change a person decides.
		Prunable: false,
	}
}

// Target reads the declared home as the reference it is: `contents/<key>`. The
// value the site module stores is the page's slug, which is what the key means
// to its owner, so the alias comes off and the key goes through the same
// function the content writer puts in seed.Resource — the two writers spell a
// content record one way because they ask the same owner. Handing on the file's
// own spelling would have site.Save accept a home slug no page wears, and a
// visitor at "/" reading a settings row about an address nothing serves.
func (siteSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	ref := seedText(r.Fields["homeSlug"])
	alias, key, found := strings.Cut(ref, "/")
	if !found || alias != "contents" || key == "" {
		return seed.Target{}, fmt.Errorf("homeSlug names the page a site opens on as contents/<slug>, not %q", ref)
	}
	return seed.Target{Fields: map[string]any{"homeSlug": contentcontracts.Slugify(key)}}, nil
}

func (w *siteSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], _ seed.Key, _ bool) (seed.Snapshot, error) {
	settings, err := w.sites.Settings(ctx, tx)
	if err != nil {
		return seed.Snapshot{}, err
	}
	// Present means "this tenant has a site row", not "the module answered": it
	// answers defaults for a tenant that saved nothing, and a run that read those
	// defaults as an existing record would call Save a write over nothing. An id
	// is what says a row was saved.
	return seed.Snapshot{Present: settings.ID != uuid.Nil, ID: settings.ID,
		Fields: map[string]any{"homeSlug": settings.HomeSlug}}, nil
}

// Create and Update are the same three lines, and that is the point: there is
// one site row per tenant, the module's Settings answers it whether or not
// anybody has saved anything, and Save is the only door that writes it — the
// door the settings screen uses, with the event that says the site moved.
func (w *siteSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	return w.write(ctx, tx, seedText(t.Fields["homeSlug"]))
}

func (w *siteSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], _ seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	return w.write(ctx, tx, seedText(t.Fields["homeSlug"]))
}

func (w *siteSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("site: a tenant always has a site; this seed unsets no home page")
}

// write reads the settings as they are now, changes the one field the seed
// manages, and saves. Reading first is what keeps a person's theme, title and
// timestamps out of a run that never mentioned them.
func (w *siteSeeder) write(ctx context.Context, tx db.Tx[db.Tenant], slug string) (seed.Snapshot, error) {
	settings, err := w.sites.Settings(ctx, tx)
	if err != nil {
		return seed.Snapshot{}, err
	}
	settings.HomeSlug = slug
	saved, err := w.sites.Save(ctx, tx, settings)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seed.Snapshot{Present: true, ID: saved.ID,
		Fields: map[string]any{"homeSlug": saved.HomeSlug}}, nil
}

// userSeeder seeds people: an invitation through the user module, the roles the
// file names, and — when the deployment names a demo password — the password the
// demo walkthrough signs in with. Roles and password are the module's own
// commands, SetRoles and SetPassword, so a seeded person holds what a person in
// the admin screens would hold and the trail says which command gave it.
//
// A person is created and never renamed: an invitation is the write the user
// module offers a seed, and a name a person chose for themselves is theirs from
// then on. Passwords are never in a seed file — the loader refuses a field named
// for one — and never in a plan line, which prints keys and addresses only.
type userSeeder struct {
	users usercontracts.Service
	// demoPassword is config's Demo.Password: the one password a demonstration's
	// people sign in with. It is the deployment's answer to a record that asks for
	// a credential, and an empty one asks the run to mint one per person and hand
	// it to the caller that ran it — see commands and mintedCredential. The loader
	// refuses any seed field named for a password, so this value has exactly one
	// door into a seed run.
	demoPassword string
}

// signInDeclared is the one word a demo record and the person it names agree on.
// In the file it is the ask — this person is one the demonstration signs in as.
// In the snapshot it is the answered fact — that person holds a sign-in
// credential. The same word on both sides is what makes a rerun of a person who
// has one read as unchanged, and it names neither a secret nor a hash.
const signInDeclared = "demo"

func (userSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "users", Module: "user", Entity: "user",
		NaturalKey: "email", WriteGrant: usercontracts.PermissionUserManage,
		// An address is the record's key and the module owns its spelling, so a
		// file that writes `Person@Example.test` declares the person the module
		// stores as `person@example.test` — one record, one mapping, one run.
		CanonicalKey: usercontracts.CanonicalEmail,
		// No delete path, deliberately: deactivating a person is not undoing an
		// invitation, and a file that stops declaring an address must not be able
		// to switch a person off.
		Prunable: false,
	}
}

func (w userSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	fields := map[string]any{"email": usercontracts.CanonicalEmail(r.Key), "displayName": seedText(r.Fields["displayName"])}
	roles, err := seedRoles_(r.Fields["roles"])
	if err != nil {
		return seed.Target{}, err
	}
	fields["roles"] = roles
	// The ask comes from the file, and not from whether this deployment happens to
	// name a demo password: a record that asks for a sign-in asks whether or not
	// the answer has arrived yet, which is what makes a deployment that sets
	// PLATFORMKIT_DEMO_PASSWORD after provisioning a tenant still able to give its
	// demo people a credential. See signInHeld for the other side.
	if seedText(r.Fields["signIn"]) == signInDeclared {
		fields["signIn"] = signInDeclared
	}
	return seed.Target{Fields: fields}, nil
}

// signInHeld is the snapshot's whole answer about sign-in: does this person hold
// a credential at all. It reads only whether the module has a hash, and never
// the hash, a comparison against one, or anything that leaves this function
// other than one word — the same shape the plan prints and no more.
//
// "Any credential", and not "the demo credential", is the point. A person who
// chose their own password holds a credential, so the record reads as settled
// and the run that comes round again writes no password at all: docs/seed.md
// promises that a rerun never resets a person's password, and the only way to
// keep that promise without a second record of who set what is to ask the row
// whether anybody has.
func signInHeld(row *usercontracts.User) string {
	if row.PasswordHash != "" {
		return signInDeclared
	}
	return ""
}

func (w userSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, _ bool) (seed.Snapshot, error) {
	var (
		row *usercontracts.User
		err error
	)
	if key.RecordID != uuid.Nil {
		row, err = w.users.Get(ctx, tx, key.RecordID)
	} else {
		row, err = w.users.ByEmail(ctx, tx, key.Value)
	}
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	fields := map[string]any{
		"email": row.Email, "displayName": row.DisplayName,
		// The stored answer, sorted the same way the declared one is, so a file
		// that lists roles in another order is the same declaration.
		"roles":  seedRolesSorted(row.Roles),
		"signIn": signInHeld(row),
	}
	return seed.Snapshot{Present: true, ID: row.ID, Fields: fields}, nil
}

func (w userSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	row, err := w.users.Invite(ctx, tx, seedText(t.Fields["email"]), seedText(t.Fields["displayName"]))
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, row, t)
}

// Update touches the two facts a seed owns about a person — the roles the file
// names, and the demo sign-in the deployment offers — and refuses the rename it
// was asked not to do. A person's name is theirs; a demo file that changed one
// fails here, loudly, rather than overwriting whoever that address belongs to.
func (w userSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	if want, have := seedText(t.Fields["displayName"]), seedText(cur.Fields["displayName"]); want != have {
		return seed.Snapshot{}, fmt.Errorf(
			"user owns a person after the invitation: this seed changes no name (had %q, the file asks %q)", have, want)
	}
	row, err := w.users.Get(ctx, tx, cur.ID)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, row, t)
}

// commands runs the module's own role and password commands for one record.
// Both are idempotent in the way the seed needs: SetRoles with the same set
// writes and publishes nothing, and a SetPassword is only reached when the
// record's sign-in fact differs, so a second run of the same files writes no
// password at all.
func (w userSeeder) commands(ctx context.Context, tx db.Tx[db.Tenant], row *usercontracts.User, t seed.Target) (seed.Snapshot, error) {
	roles, err := seedRoles_(t.Fields["roles"])
	if err != nil {
		return seed.Snapshot{}, err
	}
	// The set the file declares, whenever it differs from the set the person
	// holds — including when what it declares is nothing. A file that takes a
	// role back says `roles: []`, and a writer that skipped an empty list would
	// keep the grant the declaration removed, forever, and call it unchanged.
	// Taking the last administrator away is the user module's own refusal, and
	// this run commits nothing when it says so.
	if !slices.Equal([]string(row.Roles), roles) {
		if row, err = w.users.SetRoles(ctx, tx, row.ID, roles); err != nil {
			return seed.Snapshot{}, err
		}
	}
	// The credential a seed gives is the one a person does not have yet. Once a
	// row holds a hash the answer is settled — see signInHeld — so the deployment
	// password reaches an invited person and never overwrites a chosen one.
	if seedText(t.Fields["signIn"]) == signInDeclared && row.PasswordHash == "" {
		password := w.demoPassword
		if password == "" {
			// No deployment answer means this demonstration names its people but
			// not their credential, and an invited person who cannot be signed in
			// as is not a walkthrough. So the run mints one for this person and
			// prints it once, the way bootstrap does for the first administrator.
			if password, err = generatePassword(); err != nil {
				return seed.Snapshot{}, err
			}
			// The minted secret goes to whoever asked for this run and not to the
			// process's output — see mintedCredential. A run nothing is collecting
			// for keeps only the hash, which is the honest answer for a create
			// transaction with no caller on the other side to receive it.
			mintInto(ctx, row.Email, password)
		}
		if err := w.users.SetPassword(ctx, tx, row.ID, password); err != nil {
			return seed.Snapshot{}, err
		}
		if row, err = w.users.Get(ctx, tx, row.ID); err != nil {
			return seed.Snapshot{}, err
		}
	}
	fields := map[string]any{
		"email": row.Email, "displayName": row.DisplayName,
		"roles":  seedRolesSorted(row.Roles),
		"signIn": signInHeld(row),
	}
	return seed.Snapshot{Present: true, ID: row.ID, Fields: fields}, nil
}

func (w userSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("user: deactivating a person is not undoing an invitation")
}

// taskSeeder seeds the work a demonstration opens on. A task is created through
// task.Spec — the module's own write core, the one its POST route runs — and
// assigned through its Assign command, so a seeded row arrives with the same
// validation and the same task.created and task.assigned events a person's click
// produces. The title is the natural key the record is found by, because a task
// is named by its one line and the module says so.
type taskSeeder struct{ svc taskcontracts.Service }

func (taskSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "tasks", Module: "task", Entity: "task",
		NaturalKey: "title", WriteGrant: taskcontracts.PermissionTaskUpdate,
		// No delete path: resolving or closing a task is a person's account of
		// their own work, and a file that stopped declaring a title has no
		// business writing that account.
		Prunable: false,
		// The assignee is a person of these same files, named as
		// users/<address>, so the orderer writes the people before the work that
		// names them and a typo in an address refuses at the record.
		References: []seed.Reference{{Resource: "tasks", Path: "fields/assignee", Target: "users"}},
	}
}

func (taskSeeder) Target(ctx context.Context, r seed.Record, resolved map[string]uuid.UUID, now time.Time) (seed.Target, error) {
	priority := seedText(r.Fields["priority"])
	if priority == "" {
		priority = taskcontracts.PriorityNormal
	}
	assignee := uuid.Nil
	if ref := seedText(r.Fields["assignee"]); ref != "" {
		id, known := resolved[ref]
		if !known {
			return seed.Target{}, fmt.Errorf("assignee %q names no person this seed knows", ref)
		}
		// Assigning is a person's act in this application, and it is decided twice:
		// by the grant the seed's authorizer asks the auth module about, and by
		// policy/task.rego, which is asked with the person as its actor and refuses a
		// run that carries nobody (modules/task/internal/policy.go). A tenant created
		// with its own seed in the same transaction has no person yet, so what this
		// run may ask for is the work, unassigned, and the operator's later run — the
		// one `make seed` names a person for — is what assigns it. Naming the
		// assignee here anyway would put a CREATE line in the plan for a write this
		// run cannot make, which is the thing a plan exists not to do.
		if _, hasPerson := tenancy.PrincipalFrom(ctx); hasPerson {
			assignee = id
		}
	}
	target := seed.Target{Fields: map[string]any{
		"title": seedText(r.Fields["title"]), "priority": priority, "assignee": assignee,
	}}
	// A declared deadline reaches its owner. `dueAt: "+3d"` is the seed format's
	// relative date and the run's clock is what resolves it — the same instant the
	// row's own timestamps came from — so the demonstration's work is due when the
	// file that opened it says it is. It is a create-only fact for the reason
	// kit/seed/decide.go gives: a deadline is a person's field once the task
	// exists, and a value that moves with the clock is never reconciled. A date the
	// grammar does not read is refused here, at the record's own line, and the run
	// writes nothing.
	if expr := seedText(r.Fields["dueAt"]); expr != "" {
		due, err := seed.ResolveDate(now, expr, false)
		if err != nil {
			return seed.Target{}, fmt.Errorf("dueAt: %w", err)
		}
		target.CreateOnly = map[string]any{"dueAt": due}
	}
	return target, nil
}

func (taskSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, forUpdate bool) (seed.Snapshot, error) {
	id := key.RecordID
	if id == uuid.Nil {
		rows, _, err := crud.List[*taskcontracts.Task](tx, crud.Query{Limit: 1, Filter: map[string]any{"title": key.Value}})
		if err != nil {
			return seed.Snapshot{}, err
		}
		if len(rows) == 0 {
			return seed.Snapshot{}, nil
		}
		id = rows[0].ID
	}
	var (
		row *taskcontracts.Task
		err error
	)
	if forUpdate {
		row, err = crud.GetForUpdate[*taskcontracts.Task](tx, id)
	} else {
		row, err = crud.Get[*taskcontracts.Task](tx, id)
	}
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	return assigned(row), nil
}

func (w taskSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	row, err := task.Spec.CreateRow(ctx, tx, &taskcontracts.Task{
		Title: seedText(t.Fields["title"]), Priority: seedText(t.Fields["priority"]),
		DueAt: seedTime(t.CreateOnly["dueAt"]),
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.assign(ctx, tx, assigned(row), t)
}

// Update changes the title and priority and, where the file names a different
// person, assigns. The title is in the patch because the file declares it as a
// field of the record and the record is named by its key, not by its title: a
// run that says the tour task is called "Edited title" means the row it already
// wrote to be called something else. The deadline is not in the patch, and for
// the opposite reason: the file declared it as the record's creation date plus a
// span (see Target), and a person who moved it through the task screen moved it
// for good.
func (w taskSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	row, err := task.Spec.UpdateRow(ctx, tx, cur.ID, map[string]any{
		"title": seedText(t.Fields["title"]), "priority": seedText(t.Fields["priority"]),
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.assign(ctx, tx, assigned(row), t)
}

func (w taskSeeder) assign(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	want, _ := t.Fields["assignee"].(uuid.UUID)
	if want == uuid.Nil || want == cur.Fields["assignee"] {
		return cur, nil
	}
	row, err := w.svc.Assign(ctx, tx, cur.ID, want)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return assigned(row), nil
}

func (taskSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("task: resolving a task is not undoing one")
}

// assigned is a task's canonical seed state: the line a list shows, the urgency
// beside it, and who is responsible — nobody being uuid.Nil, so an unassigned row
// and a record that names no person agree.
func assigned(row *taskcontracts.Task) seed.Snapshot {
	assignee := uuid.Nil
	if row.AssigneeID != nil {
		assignee = *row.AssigneeID
	}
	return seed.Snapshot{Present: true, ID: row.ID,
		Fields: map[string]any{"title": row.Title, "priority": row.Priority, "assignee": assignee}}
}

// fileSeeder seeds the one image a demonstration carries. The bytes live beside
// the record that names them, in the same embedded tree the records came from, and
// they go through the file module's own Upload — the door the upload form uses —
// so a seeded file is counted, hashed, quota-checked and announced by
// file.uploaded exactly as an upload is. No row is written by hand here.
type fileSeeder struct{ svc filecontracts.Service }

// seedAssets is that tree, spelled from the record's own key: `asset` names a
// file beside the YAML, the loader checked it is a regular file, and the name the
// record gives the upload is the last part of it. A Target carries a value and not
// a source path, so this is where the two meet — and a record whose asset is not
// where its name says it is fails here, rather than quietly uploading something
// else with the same name.
const seedAssets = "seed/demo/assets/"

func (fileSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "files", Module: "file", Entity: "file",
		// No natural key: a file's identity is its id, and the name a person reads
		// is not unique. The seed finds its own row through seed_keys alone.
		WriteGrant: filecontracts.PermissionFileManage,
		// No delete path: the bytes behind a file may be read by something that
		// linked to them, and a YAML record that stopped existing is not the event
		// that decides they should go.
		Prunable: false,
	}
}

func (fileSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	if r.Asset == "" {
		return seed.Target{}, errors.New("a file record names the asset beside it")
	}
	name := path.Base(r.Asset)
	if name != seedText(r.Fields["name"]) && seedText(r.Fields["name"]) != "" {
		return seed.Target{}, fmt.Errorf("a file record's key names its asset %q, not %q", name, seedText(r.Fields["name"]))
	}
	return seed.Target{Fields: map[string]any{
		"name": name, "contentType": mime.TypeByExtension(strings.ToLower(path.Ext(name))),
	}}, nil
}

func (fileSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, _ bool) (seed.Snapshot, error) {
	if key.RecordID == uuid.Nil {
		// Without provenance there is nothing to read: a name is not an identity
		// here, and guessing which of a tenant's files "looks like" a seeded one
		// would be a seed that deleted a person's upload.
		return seed.Snapshot{}, nil
	}
	var (
		row *filecontracts.File
		err error
	)
	row, err = crud.Get[*filecontracts.File](tx, key.RecordID)
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seed.Snapshot{Present: true, ID: row.ID,
		Fields: map[string]any{"name": row.Name, "contentType": row.ContentType}}, nil
}

func (w fileSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	name, _ := t.Fields["name"].(string)
	body := seedAssets + name
	file, err := seedFiles.Open(body)
	if err != nil {
		return seed.Snapshot{}, fmt.Errorf("the seed's own asset %q is not in the binary: %w", body, err)
	}
	defer file.Close()
	// The accessor answers with the run's own transaction, because there is
	// nothing to stream: the bytes are already in the binary, so no connection
	// stands open while they arrive, which is the only reason Upload takes an
	// accessor rather than a transaction.
	row, err := w.svc.Upload(ctx, func(context.Context) (db.Tx[db.Tenant], error) { return tx, nil },
		filecontracts.Upload{
			Name: name, ContentType: seedText(t.Fields["contentType"]),
			Visibility: filecontracts.VisibilityPublic, Declared: -1, Body: file,
		})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seed.Snapshot{Present: true, ID: row.ID,
		Fields: map[string]any{"name": row.Name, "contentType": row.ContentType}}, nil
}

// Update refuses, and the refusal is the design: a rerun reads back the name and
// the media type it stored, finds them unchanged, and never reaches here. The
// alternative — re-uploading the asset every run — would leave a new row and new
// bytes on every deploy, and the old ones behind.
func (fileSeeder) Update(context.Context, db.Tx[db.Tenant], seed.Snapshot, seed.Target) (seed.Snapshot, error) {
	return seed.Snapshot{}, errors.New("file: the seed uploads an asset and writes no bytes over it")
}

func (fileSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("file: a seeded upload is a person's file from the moment it exists")
}

// seedRoles_ reads the record's roles field as the sorted list both sides of the
// comparison use. YAML hands a list as []any of strings; the module hands
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

// seedRolesSorted is the same list in the same order, with the empty answers
// collapsed to one value: an invited person holds no roles, and a file that
// names none asks for no roles, and those two sentences agree only if both sides
// print the same empty slice.
func seedRolesSorted(roles []string) []string {
	out := slices.Clone([]string(roles))
	if out == nil {
		out = []string{}
	}
	slices.Sort(out)
	return out
}

// seedText is a seed field read as text. A file that puts a number or a list where a
// sentence belongs reads as empty, and the owner's own validation is what
// refuses it — with its own text, at its own boundary, rather than through a
// second type check invented here.
func seedText(v any) string {
	s, _ := v.(string)
	// Trimmed, because the owners trim: content stores a title without the
	// spaces around it, so a target that kept them would differ from the stored
	// row on every run forever. The canonical value is the one the owner keeps.
	return strings.TrimSpace(s)
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
