package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
)

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
	text, err := seedTexts(r, "kind", "title", "body")
	if err != nil {
		return seed.Target{}, err
	}
	// No kind written is the module's default for the question; a kind the file
	// wrote — as a number, as nothing at all — is a declaration, and it goes to the
	// check below, which answers it as content's own enum.
	kind, given := text["kind"]
	if !given {
		kind = contentcontracts.KindPage
	}
	if kind != contentcontracts.KindPage && kind != contentcontracts.KindPost {
		return seed.Target{}, fmt.Errorf("kind %q is not %s or %s", kind, contentcontracts.KindPage, contentcontracts.KindPost)
	}
	// The body is reconciled in the form the module stores it. content normalises
	// rich text on write — kit/richtext.Prepare runs in the same write core this
	// writer calls — and the stored form ends its last line with a newline, so a
	// declaration read with the trailing whitespace trimmed off never equals what
	// the row holds and the page is patched on every run forever. Asking the
	// module's own normaliser here has a second benefit: a body it would refuse
	// (a level-1 heading, a construct it cannot store) is refused at the file and
	// line that declared it, before this run has written anything.
	body, err := richtext.Normalise(text["body"])
	if err != nil {
		return seed.Target{}, fmt.Errorf("body: %w", err)
	}
	return seed.Target{
		Fields:   map[string]any{"slug": contentcontracts.Slugify(r.Key), "title": text["title"], "body": body, "kind": kind},
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
	row, err := content.Spec.CreateRow(unactor(ctx), tx, &contentcontracts.Content{
		Slug: t.Fields["slug"].(string), Title: t.Fields["title"].(string),
		Body: t.Fields["body"].(string), Kind: t.Fields["kind"].(string),
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.commands(ctx, tx, seeded(row), t)
}

func (w *contentSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	// The slug is how the record was found, so it is not in the patch: a run that
	// renamed a page would be a run that deleted one page and created another.
	row, err := content.Spec.UpdateRow(unactor(ctx), tx, cur.ID, map[string]any{
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
	row, err := w.svc.Publish(unactor(ctx), tx, cur.ID)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return seeded(row), nil
}

// unactor is the context an owner write goes through when the run is a seed.
// content's PublishBy duty separates the person who wrote a page from the person
// who puts it in front of the world; the module's own rule says content nobody is
// credited with — a seed, an import, a row a job wrote where no person is acting —
// has no authorship to separate, so it publishes. A seed run is that case: nobody
// signed in (the outbox row holds no actor), and the person the run served travels
// in the attribution's initiator, beside the file and line that asked for the
// write. Handing the owner an actor as well would credit the page to that person
// and then refuse the publish command this file declares, which is what the run is
// here to run.
func unactor(ctx context.Context) context.Context {
	return tenancy.WithActor(ctx, uuid.Nil)
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
	text, err := seedTexts(r, "homeSlug")
	if err != nil {
		return seed.Target{}, err
	}
	ref := text["homeSlug"]
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
	return w.write(ctx, tx, t.Fields["homeSlug"].(string))
}

func (w *siteSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], _ seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	return w.write(ctx, tx, t.Fields["homeSlug"].(string))
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
