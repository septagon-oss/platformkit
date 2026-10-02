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

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
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
	auth         authcontracts.Auth
	demoPassword string
}

func (p *seedProvisioner) OnTenantCreate(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
	if p.users == nil || p.contents == nil || p.sites == nil || p.auth == nil {
		return errors.New("seed: the creation hook ran before compose filled its owners")
	}
	service, err := seed.New(seed.Deps{
		Files: seedFiles, Root: "seed", Clock: seedClock{},
		Writers: []seed.Writer{
			&contentSeeder{svc: p.contents},
			&siteSeeder{sites: p.sites},
			&userSeeder{users: p.users, demoPassword: p.demoPassword},
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
// to write: the roles the run carries, the permissions those roles hold, and the
// resource's own permission compared between them — the same three reads
// kit/httpx makes of a request, in the seed's own transaction.
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
	caller, ok := tenancy.PrincipalFrom(ctx)
	if !ok || len(caller.Roles) == 0 {
		// The one run with nobody to ask is the tenant's own creation, and the
		// conditions that make it safe are state, not a claim: see
		// seed.Service.ApplyProvisioned (the tenant holds no seeded record and
		// every record the files name is still absent) and seedProvisioner below
		// (the tenant holds no person). Together they mean this run can only
		// give a tenant that does not exist yet the records every tenant of this
		// application is created with. Anything else names a person.
		if seed.Provisioning(ctx) {
			return nil
		}
		return fmt.Errorf("seed: %s/%s: this run carries no person and no roles", r.Module, r.Entity)
	}
	held, err := g.auth.Permissions(ctx, tx, caller.Roles)
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
func seedActor(ctx context.Context, users usercontracts.Service, tx db.Tx[db.Tenant], email string) (context.Context, error) {
	person, err := users.ByEmail(ctx, tx, email)
	if err != nil {
		return ctx, fmt.Errorf("seed: this run writes as %s, who is not a person of this tenant: %w", email, err)
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
// record by, so the seed finds a page the way a person does: by its address.
type contentSeeder struct{ svc contentcontracts.Service }

func (contentSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "contents", Module: "content", Entity: "content",
		NaturalKey: "slug", WriteGrant: contentcontracts.PermissionContentManage,
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
		Fields:   map[string]any{"slug": r.Key, "title": seedText(r.Fields["title"]), "body": seedText(r.Fields["body"]), "kind": kind},
		Commands: seedCommands(r, "publish"),
	}, nil
}

func (w *contentSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, forUpdate bool) (seed.Snapshot, error) {
	id := key.RecordID
	if id == uuid.Nil {
		// Provenance has no id, so this is the natural-key lookup. A seed run may
		// meet a manually created page with the same slug; the service decides
		// whether that row is its own to touch, and this only says what is there.
		rows, _, err := crud.List[*contentcontracts.Content](tx, crud.Query{Limit: 2, Filter: map[string]any{"slug": key.Value}})
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
		// No delete path: the module offers no way to un-have a site, and
		// blanking a tenant's home slug is a change a person decides.
		Prunable: false,
	}
}

func (siteSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	slug := seedText(r.Fields["homeSlug"])
	if slug == "" {
		return seed.Target{}, errors.New("a site record names the slug it opens on")
	}
	return seed.Target{Fields: map[string]any{"homeSlug": slug}}, nil
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
	// people sign in with. Empty means this installation offers no demo sign-in,
	// the record's signIn field is then neither asked for nor compared, and the
	// people arrive invited rather than active — see Limits in kit/seed/README.md.
	demoPassword string
}

// signInDeclared is the value a demo record names for its signIn field. The
// word, and not a secret, is what the plan and the snapshot compare.
const signInDeclared = "demo"

func (userSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "users", Module: "user", Entity: "user",
		NaturalKey: "email", WriteGrant: usercontracts.PermissionUserManage,
		// No delete path, deliberately: deactivating a person is not undoing an
		// invitation, and a file that stops declaring an address must not be able
		// to switch a person off.
		Prunable: false,
	}
}

func (w userSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	fields := map[string]any{"email": r.Key, "displayName": seedText(r.Fields["displayName"])}
	roles, err := seedRoles_(r.Fields["roles"])
	if err != nil {
		return seed.Target{}, err
	}
	fields["roles"] = roles
	if w.demoPassword != "" {
		fields["signIn"] = signInDeclared
	}
	return seed.Target{Fields: fields}, nil
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
		"roles": seedRolesSorted(row.Roles),
	}
	if w.demoPassword != "" {
		// A fact about the row, told in one word: can this person sign in with
		// the demo password the deployment named. The password itself and its
		// hash never leave the module that holds them.
		if row.CheckPassword(w.demoPassword) {
			fields["signIn"] = signInDeclared
		} else {
			fields["signIn"] = ""
		}
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
	if len(roles) > 0 && !slices.Equal([]string(row.Roles), roles) {
		if row, err = w.users.SetRoles(ctx, tx, row.ID, roles); err != nil {
			return seed.Snapshot{}, err
		}
	}
	if seedText(t.Fields["signIn"]) == signInDeclared && !row.CheckPassword(w.demoPassword) {
		if err := w.users.SetPassword(ctx, tx, row.ID, w.demoPassword); err != nil {
			return seed.Snapshot{}, err
		}
		if row, err = w.users.Get(ctx, tx, row.ID); err != nil {
			return seed.Snapshot{}, err
		}
	}
	fields := map[string]any{
		"email": row.Email, "displayName": row.DisplayName,
		"roles": seedRolesSorted(row.Roles),
	}
	if w.demoPassword != "" {
		if row.CheckPassword(w.demoPassword) {
			fields["signIn"] = signInDeclared
		} else {
			fields["signIn"] = ""
		}
	}
	return seed.Snapshot{Present: true, ID: row.ID, Fields: fields}, nil
}

func (w userSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("user: deactivating a person is not undoing an invitation")
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
