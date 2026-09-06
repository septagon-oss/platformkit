package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/contracts/contenttest"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

var acme = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}

// errRollback ends a conformance case's transaction without committing it.
var errRollback = errors.New("rolled back on purpose")

const outbox = "platformkit_outbox"

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	contenttest.RunService(t, func(t *testing.T, run func(contenttest.Fixture)) {
		_, conn := dbtest.Schema(t)
		svc := internal.NewService()
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(contenttest.Fixture{Ctx: ctx, Tx: tx, Service: svc,
				Seed: func(c *contracts.Content) uuid.UUID {
					if err := crud.Create(ctx, tx, c); err != nil {
						t.Fatalf("seed content: %v", err)
					}
					return c.ID
				},
				Published: func() []string {
					var names []string
					err := tx.DB().Raw(`SELECT name FROM `+outbox+` WHERE tenant_id = ? ORDER BY created_at, id`, acme.ID).
						Scan(&names).Error
					if err != nil {
						t.Fatalf("read the outbox: %v", err)
					}
					return names
				}})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// TestTheCommandsPublishInTheCallersTransaction is what the conformance suite
// cannot see, because the fake has no outbox: a state change and the event that
// describes it are one row each in one transaction. It also pins the idempotent
// paths as silent.
func TestTheCommandsPublishInTheCallersTransaction(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		page := &contracts.Content{Slug: "about-us", Title: "About us", Body: "hello"}
		if err := crud.Create(ctx, tx, page); err != nil {
			return err
		}
		// Each command twice: the second is the retry, and says nothing.
		for _, command := range []func() error{
			func() error { _, err := svc.Publish(ctx, tx, page.ID); return err },
			func() error { _, err := svc.Unpublish(ctx, tx, page.ID); return err },
			func() error { _, err := svc.Archive(ctx, tx, page.ID); return err },
		} {
			for range 2 {
				if err := command(); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the commands: %v", err)
	}

	for _, name := range []string{contracts.EventPublished, contracts.EventUnpublished, contracts.EventArchived} {
		var count int
		err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2`, name, acme.ID).Scan(&count)
		if err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if count != 1 {
			t.Errorf("%s was published %d times, want once", name, count)
		}
	}
	var total int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_outbox`).Scan(&total); err != nil {
		t.Fatalf("count the outbox: %v", err)
	}
	if total != 3 {
		t.Errorf("the outbox holds %d events, want the three the commands published", total)
	}
}

// TestTheDatabaseKeepsPublishedAndItsTimeTogether. The entity's Validate says
// it and this is the half that is true of a row no Go code wrote: a CHECK
// constraint, so an UPDATE straight at the column cannot leave a published page
// with no publication time or a draft claiming one.
func TestTheDatabaseKeepsPublishedAndItsTimeTogether(t *testing.T) {
	_, conn := dbtest.Schema(t)

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		page := &contracts.Content{Slug: "about-us", Title: "About us"}
		if err := crud.Create(ctx, tx, page); err != nil {
			return err
		}
		return tx.DB().Exec(`UPDATE contents SET status = 'published' WHERE id = ?`, page.ID).Error
	})
	if err == nil || !strings.Contains(err.Error(), "contents_published") {
		t.Errorf("publishing a row behind the module's back = %v; the constraint is what refuses it", err)
	}
}

// TestASlugIsUniqueWithinTheTenantAndReleasedByADelete. Two pages cannot share
// a name, and a deleted one gives its name back — which is the partial index
// rather than anything Go remembers.
func TestASlugIsUniqueWithinTheTenantAndReleasedByADelete(t *testing.T) {
	_, conn := dbtest.Schema(t)
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}

	var first uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		page := &contracts.Content{Slug: "About Us", Title: "About us"}
		if err := crud.Create(ctx, tx, page); err != nil {
			return err
		}
		first = page.ID
		return nil
	})
	if err != nil {
		t.Fatalf("the first page: %v", err)
	}
	// The same name, spelled differently, is the same name. It is its own
	// transaction because a failed write aborts the one it happened in.
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, &contracts.Content{Slug: "about--us", Title: "Again"})
	})
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a second page at one slug = %v, want a conflict", err)
	}

	// Another tenant's site has its own names.
	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, &contracts.Content{Slug: "about-us", Title: "About Globex"})
	})
	if err != nil {
		t.Errorf("globex's own about-us: %v; a slug is unique within a tenant and not across the installation", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := crud.Delete[*contracts.Content](tx, first, true); err != nil {
			return err
		}
		return crud.Create(ctx, tx, &contracts.Content{Slug: "about-us", Title: "The new one"})
	})
	if err != nil {
		t.Errorf("reusing a deleted page's slug: %v; the index is partial so a delete releases the name", err)
	}
}

// TestTheAuthorIsTheCallerAndNotTheBody. Validate stamps it from the actor on
// the context, which is the same place kit/events reads the actor of an event
// from, so a body that claims somebody else's byline is overwritten rather than
// believed.
func TestTheAuthorIsTheCallerAndNotTheBody(t *testing.T) {
	_, conn := dbtest.Schema(t)
	me, impostor := uuid.New(), uuid.New()

	var mine, seeded contracts.Content
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), me)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		mine = contracts.Content{Slug: "mine", Title: "Mine", AuthorID: impostor}
		if err := crud.Create(ctx, tx, &mine); err != nil {
			return err
		}
		// A caller with no actor — a seed, a job — leaves it unattributed
		// rather than attributing it to nobody in particular.
		seeded = contracts.Content{Slug: "seeded", Title: "Seeded"}
		return crud.Create(context.Background(), tx, &seeded)
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if mine.AuthorID != impostor {
		t.Errorf("the author is %s, want the id the body claimed: Validate only stamps an empty one", mine.AuthorID)
	}
	if seeded.AuthorID != uuid.Nil {
		t.Errorf("content written with no caller is credited to %s", seeded.AuthorID)
	}

	// And the ordinary path: a body that says nothing is credited to whoever
	// wrote it.
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		page := &contracts.Content{Slug: "ours", Title: "Ours"}
		if err := crud.Create(ctx, tx, page); err != nil {
			return err
		}
		if page.AuthorID != me {
			t.Errorf("the author is %s, want the caller %s", page.AuthorID, me)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
}

// TestRenderStripsWhatAMarkdownBodyShouldNotCarry is the two layers, checked
// together: goldmark leaves raw HTML out and bluemonday refuses what is left.
func TestRenderStripsWhatAMarkdownBodyShouldNotCarry(t *testing.T) {
	body := "# Hello\n\n<script>alert('xss')</script>\n\n" +
		"<img src=x onerror=\"alert('xss')\">\n\n" +
		"[click me](javascript:alert('xss'))\n\n" +
		"<a href=\"https://example.com\" onclick=\"steal()\">a link</a>\n\n" +
		"Some **bold** text and a [real link](https://example.com/docs).\n"
	html, err := contracts.Render(body)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, forbidden := range []string{"<script", "onerror", "onclick", "javascript:", "alert("} {
		if strings.Contains(html, forbidden) {
			t.Errorf("the rendered HTML carries %q:\n%s", forbidden, html)
		}
	}
	for _, wanted := range []string{"<h1", "Hello", "<strong>bold</strong>", `href="https://example.com/docs"`} {
		if !strings.Contains(html, wanted) {
			t.Errorf("the rendered HTML is missing %q:\n%s", wanted, html)
		}
	}
}
