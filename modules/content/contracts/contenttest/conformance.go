// Package contenttest is the conformance suite for contracts.Service, and a
// fake that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification of the lifecycle written as executable cases; the real service
// and the fake both run it, so "the fake behaves like the real thing" is a test
// result rather than a hope.
package contenttest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/content/contracts"

	"context"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a store to put content in.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Seed stores content and returns the id it was given. It is the one thing
	// the suite cannot do through the interface, because the interface is the
	// lifecycle and creating a page is kit/rest's five routes.
	Seed func(*contracts.Content) uuid.UUID
	// Published is the events the implementation has published so far, in
	// order. Half of what the lifecycle promises is silence: publishing what is
	// already published is a second click on a button, not a second
	// publication, and a subscriber must not hear about it.
	Published func() []string
}

func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; repeating a command changes nothing, so it says nothing", what, after[before:])
	}
}

func (f Fixture) one(t *testing.T, what, want string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	got := f.Published()[before:]
	if len(got) != 1 || got[0] != want {
		t.Errorf("%s published %v, want [%s]", what, got, want)
	}
}

// Harness builds one Fixture and calls run with it, because the real service's
// fixture is a transaction and a transaction is a scope somebody has to close.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	for name, run := range cases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// draft is content in the state everything starts in.
func draft(slug string) *contracts.Content {
	return &contracts.Content{Slug: slug, Title: "About us", Body: "# About\n\nWe make things.", Kind: contracts.KindPage}
}

// published seeds content and publishes it, which is two steps everywhere.
func published(t *testing.T, f Fixture, slug string) *contracts.Content {
	t.Helper()
	out, err := f.Service.Publish(f.Ctx, f.Tx, f.Seed(draft(slug)))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return out
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"publishing serves it and records when": func(t *testing.T, f Fixture) {
			var got *contracts.Content
			f.one(t, "publishing a draft", contracts.EventPublished, func() { got = published(t, f, "about-us") })
			if got.Status != contracts.StatusPublished {
				t.Errorf("status is %q, want %q", got.Status, contracts.StatusPublished)
			}
			if got.PublishedAt == nil {
				t.Error("published content has no publication time")
			}
		},

		"publishing twice does not move the publication time": func(t *testing.T, f Fixture) {
			first := published(t, f, "about-us")
			var again *contracts.Content
			f.silent(t, "publishing what is already published", func() {
				var err error
				if again, err = f.Service.Publish(f.Ctx, f.Tx, first.ID); err != nil {
					t.Fatalf("the second Publish: %v", err)
				}
			})
			if !again.PublishedAt.Equal(*first.PublishedAt) {
				t.Error("the second Publish moved the publication time; a page is published once")
			}
		},

		"archived content is not published from the archive": func(t *testing.T, f Fixture) {
			id := f.Seed(draft("about-us"))
			if _, err := f.Service.Archive(f.Ctx, f.Tx, id); err != nil {
				t.Fatalf("Archive: %v", err)
			}
			_, err := f.Service.Publish(f.Ctx, f.Tx, id)
			mustBe(t, err, crud.ErrConflict)
		},

		"unpublishing clears the publication time": func(t *testing.T, f Fixture) {
			live := published(t, f, "about-us")
			var back *contracts.Content
			f.one(t, "unpublishing", contracts.EventUnpublished, func() {
				var err error
				if back, err = f.Service.Unpublish(f.Ctx, f.Tx, live.ID); err != nil {
					t.Fatalf("Unpublish: %v", err)
				}
			})
			if back.Status != contracts.StatusDraft || back.PublishedAt != nil {
				t.Errorf("it is %q/%v; published means published at a time, and this is not published",
					back.Status, back.PublishedAt)
			}
			f.silent(t, "unpublishing a draft", func() {
				if _, err := f.Service.Unpublish(f.Ctx, f.Tx, live.ID); err != nil {
					t.Fatalf("the second Unpublish: %v", err)
				}
			})
		},

		"unpublishing takes content out of the archive": func(t *testing.T, f Fixture) {
			id := f.Seed(draft("about-us"))
			if _, err := f.Service.Archive(f.Ctx, f.Tx, id); err != nil {
				t.Fatalf("Archive: %v", err)
			}
			var back *contracts.Content
			f.one(t, "unarchiving", contracts.EventUnpublished, func() {
				var err error
				if back, err = f.Service.Unpublish(f.Ctx, f.Tx, id); err != nil {
					t.Fatalf("Unpublish: %v", err)
				}
			})
			if back.Status != contracts.StatusDraft {
				t.Errorf("status is %q, want %q: the archive is where a draft came back from", back.Status, contracts.StatusDraft)
			}
		},

		"archiving keeps it and serves it to nobody": func(t *testing.T, f Fixture) {
			live := published(t, f, "about-us")
			var filed *contracts.Content
			f.one(t, "archiving", contracts.EventArchived, func() {
				var err error
				if filed, err = f.Service.Archive(f.Ctx, f.Tx, live.ID); err != nil {
					t.Fatalf("Archive: %v", err)
				}
			})
			if filed.Status != contracts.StatusArchived || filed.PublishedAt != nil {
				t.Errorf("it is %q/%v, want it archived and not published", filed.Status, filed.PublishedAt)
			}
			f.silent(t, "archiving twice", func() {
				if _, err := f.Service.Archive(f.Ctx, f.Tx, live.ID); err != nil {
					t.Fatalf("the second Archive: %v", err)
				}
			})
		},

		"only published content is served publicly": func(t *testing.T, f Fixture) {
			id := f.Seed(draft("about-us"))
			if _, err := f.Service.Public(f.Ctx, f.Tx, "about-us"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("a draft is served publicly = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.Publish(f.Ctx, f.Tx, id); err != nil {
				t.Fatalf("Publish: %v", err)
			}
			got, err := f.Service.Public(f.Ctx, f.Tx, "about-us")
			if err != nil || got.ID != id {
				t.Fatalf("the published page = %v, %v", got, err)
			}
			if _, err := f.Service.Archive(f.Ctx, f.Tx, id); err != nil {
				t.Fatalf("Archive: %v", err)
			}
			if _, err := f.Service.Public(f.Ctx, f.Tx, "about-us"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("an archived page is served publicly = %v, want ErrNotFound", err)
			}
		},

		"a slug is stored and looked up the same way": func(t *testing.T, f Fixture) {
			// The name was written with capitals and spaces; the URL that
			// reaches it is the normalised one, and so is the row.
			live := published(t, f, "About Us!")
			if live.Slug != "about-us" {
				t.Errorf("the slug is %q, want %q", live.Slug, "about-us")
			}
			if _, err := f.Service.Public(f.Ctx, f.Tx, "About  Us"); err != nil {
				t.Errorf("looking up %q: %v; a name stored one way and looked up another is a page nobody can reach", "About  Us", err)
			}
		},

		"an unknown id is not found": func(t *testing.T, f Fixture) {
			id := uuid.New()
			for what, call := range map[string]func() error{
				"Publish":   func() error { _, err := f.Service.Publish(f.Ctx, f.Tx, id); return err },
				"Unpublish": func() error { _, err := f.Service.Unpublish(f.Ctx, f.Tx, id); return err },
				"Archive":   func() error { _, err := f.Service.Archive(f.Ctx, f.Tx, id); return err },
			} {
				if err := call(); !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("%s of unknown content = %v, want ErrNotFound", what, err)
				}
			}
			if _, err := f.Service.Public(f.Ctx, f.Tx, "nothing-here"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("an unused slug = %v, want ErrNotFound", err)
			}
		},
	}
}

func mustBe(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("error is %v, want %v", got, want)
	}
}
