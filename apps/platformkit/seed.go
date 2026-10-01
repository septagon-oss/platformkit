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
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
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
			&userSeeder{users: c.users},
		},
		Authorize: seedGrants{auth: c.auth},
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
// The actor goes on the context as well, because that is where an owner's own
// validation stamps an author from, and where the event that announces a write
// finds who to name — which is how a seeded row reaches the audit trail with a
// person's id on it.
func seedActor(ctx context.Context, users usercontracts.Service, tx db.Tx[db.Tenant], email string) (context.Context, error) {
	person, err := users.ByEmail(ctx, tx, email)
	if err != nil {
		return ctx, fmt.Errorf("seed: this run writes as %s, who is not a person of this tenant: %w", email, err)
	}
	return tenancy.WithActor(tenancy.WithPrincipal(ctx, tenancy.Principal{
		UserID: person.ID, Roles: []string(person.Roles),
	}), person.ID), nil
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

// userSeeder invites people. It creates and nothing else: an invitation is the
// write the user module offers a seed, and a person's name, roles and password
// are theirs from the moment the invitation exists.
type userSeeder struct{ users usercontracts.Service }

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

func (userSeeder) Target(_ context.Context, r seed.Record, _ map[string]uuid.UUID, _ time.Time) (seed.Target, error) {
	return seed.Target{Fields: map[string]any{"email": r.Key, "displayName": seedText(r.Fields["displayName"])}}, nil
}

func (w *userSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, _ bool) (seed.Snapshot, error) {
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
	return seed.Snapshot{Present: true, ID: row.ID,
		Fields: map[string]any{"email": row.Email, "displayName": row.DisplayName}}, nil
}

func (w *userSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	row, err := w.users.Invite(ctx, tx, seedText(t.Fields["email"]), seedText(t.Fields["displayName"]))
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seed.Snapshot{Present: true, ID: row.ID, Fields: map[string]any{
		"email": row.Email, "displayName": row.DisplayName}}, nil
}

func (w *userSeeder) Update(context.Context, db.Tx[db.Tenant], seed.Snapshot, seed.Target) (seed.Snapshot, error) {
	return seed.Snapshot{}, errors.New("user owns a person after the invitation: this seed changes no name, no role and no password")
}

func (w *userSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("user: deactivating a person is not undoing an invitation")
}

// seedText is a seed field read as text. A file that puts a number or a list where a
// sentence belongs reads as empty, and the owner's own validation is what
// refuses it — with its own text, at its own boundary, rather than through a
// second type check invented here.
func seedText(v any) string {
	s, _ := v.(string)
	return s
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
