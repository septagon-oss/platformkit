// Package contenttest is the conformance suite for contracts.Service, and a
// fake that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification of the lifecycle written as executable cases; the real service
// and the fake both run it, so "the fake behaves like the real thing" is a test
// result rather than a hope.
//
// The suite is a kit/porttest description: the four operations, what each
// publishes, and which calls each must refuse. What that buys over the nine
// hand-written cases it replaces is nine more cases — an unknown row at every
// command instead of one case covering three, the retry at every command
// against the whole row rather than against one field, and the read asked
// whether it publishes anything — and nine written reasons where a case is not
// owed.
package contenttest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
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
	// Content reads one back. The suite needs it to say what "a refused command
	// wrote nothing" means: the snapshot either side of a refusal is this row,
	// rendered.
	Content func(uuid.UUID) (contracts.Content, error)
	// Published is the events the implementation has published so far, in
	// order. Half of what the lifecycle promises is silence: publishing what is
	// already published is a second click on a button, not a second
	// publication, and a subscriber must not hear about it.
	Published func() []string
}

// one runs step and fails unless it published exactly want.
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
	porttest.Run(t, Suite(h))
}

// Suite is the port, described. A consumer that wants the case names without
// running them reads porttest.Names(contenttest.Suite(h)).
func Suite(h Harness) porttest.Suite[Fixture] {
	return porttest.Suite[Fixture]{
		Port:     "contracts.Service",
		World:    h,
		Events:   func(f Fixture) []string { return f.Published() },
		Classify: classify,
		Ops: []porttest.Op[Fixture]{
			{
				Name: "Publish", Mutates: true,
				Publishes: []string{contracts.EventPublished},
				Ready:     func(_ *testing.T, f Fixture) uuid.UUID { return f.Seed(draft("about-us")) },
				Call:      publish,
				Snapshot:  snapshot,
				// The retry keeps the sentence the hand-written case used, and
				// the generated case asserts more than it did: the publication
				// time is one field of the snapshot the retry compares whole,
				// and one field of the answer it compares either side of the
				// second call, which is the assertion the sentence names.
				Names: map[porttest.Kind]string{
					porttest.Retry: "publishing twice does not move the publication time",
				},
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return publish(f, uuid.New()) }),
						Is:   is(crud.ErrNotFound)},
					{Name: "archived content is not published from the archive", Class: porttest.Immutable,
						Provoke: func(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID {
							archive(t, f, row)
							return row
						},
						Call: refuse(publish),
						Is:   is(crud.ErrConflict)},
				},
				Skip: notOwedHere,
			},
			{
				Name: "Unpublish", Mutates: true,
				Publishes: []string{contracts.EventUnpublished},
				Ready:     func(t *testing.T, f Fixture) uuid.UUID { return live(t, f, "about-us").ID },
				Call:      unpublish,
				Snapshot:  snapshot,
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return unpublish(f, uuid.New()) }),
						Is:   is(crud.ErrNotFound)},
				},
				Skip: notOwedHere,
			},
			{
				Name: "Archive", Mutates: true,
				Publishes: []string{contracts.EventArchived},
				Ready:     func(t *testing.T, f Fixture) uuid.UUID { return live(t, f, "about-us").ID },
				Call:      archiveOp,
				Snapshot:  snapshot,
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return archiveOp(f, uuid.New()) }),
						Is:   is(crud.ErrNotFound)},
				},
				Skip: notOwedHere,
			},
			{
				// A read: not asked to be idempotent, for a revision or for a
				// grant, which is why it claims none of those skips. It is
				// asked the one thing a read still owes — that it publishes
				// nothing — and the slug nobody has used.
				Name:  "Public",
				Ready: func(t *testing.T, f Fixture) uuid.UUID { return live(t, f, "about-us").ID },
				Call: func(f Fixture, row uuid.UUID) (string, error) {
					c, err := f.Content(row)
					if err != nil {
						return "nothing", err
					}
					return public(f, c.Slug)
				},
				Refusals: []porttest.Refusal[Fixture]{
					{Kind: porttest.Unknown, Name: "an unused slug is not found", Class: porttest.Immutable,
						Call: refuse(func(f Fixture, _ uuid.UUID) (string, error) { return public(f, "nothing-here") }),
						Is:   is(crud.ErrNotFound)},
				},
			},
		},
		Own: cases(),
	}
}

// notOwedHere is the three floor cases this port does not owe, with the reason
// each is not asked. They are the same for all three commands.
var notOwedHere = map[porttest.Kind]string{
	porttest.Denied: "the lifecycle takes no grant of its own: the three commands are reached " +
		"through kit/rest's routes and the public page through the site, and that is where the " +
		"grant is checked",
	porttest.Stale: "content carries no revision field, and nothing below this port owns " +
		"an optimistic-concurrency check to decline one: kit/crud.Update takes the columns " +
		"to write and kit/crud.GetForUpdate takes a row lock; neither compares a revision. " +
		"This package's TestTheStaleSkipReasonStillStatesWhatTheKernelHas watches both, so a " +
		"kernel that grows the check reddens this sentence rather than leaving it standing. " +
		"When it does, the case is the harness's to generate and the skip goes away",
	porttest.Elsewhere: "the world is one tenant's transaction, so there is no second tenant " +
		"here to make the refused call from. The fake holds the rows of the first tenant that " +
		"reaches it and answers any other with nothing, which this package's own " +
		"TestAnotherTenantReachesNothingThroughTheFake pins, and the real service runs under " +
		"row-level security, which kit/db's TestTenantIsolationIsEnforcedByPostgres and " +
		"kit/crud's TestAnotherTenantReachesNothing prove against the schema",
}

func is(want error) func(Fixture, error) bool {
	return func(_ Fixture, err error) bool { return errors.Is(err, want) }
}

// classify is this port's reading of its own refusals: an input the caller can
// retype, or a state that forbids the call however it is retyped.
func classify(err error) porttest.Class {
	switch {
	case errors.Is(err, crud.ErrInvalid):
		return porttest.Correctable
	case errors.Is(err, crud.ErrConflict), errors.Is(err, crud.ErrNotFound):
		return porttest.Immutable
	default:
		return porttest.Unclassified
	}
}

func publish(f Fixture, row uuid.UUID) (string, error) {
	got, err := f.Service.Publish(f.Ctx, f.Tx, row)
	return answer(got), err
}

func unpublish(f Fixture, row uuid.UUID) (string, error) {
	got, err := f.Service.Unpublish(f.Ctx, f.Tx, row)
	return answer(got), err
}

func archiveOp(f Fixture, row uuid.UUID) (string, error) {
	got, err := archiveErr(f, row)
	return answer(got), err
}

func public(f Fixture, slug string) (string, error) {
	got, err := f.Service.Public(f.Ctx, f.Tx, slug)
	return answer(got), err
}

func archiveErr(f Fixture, row uuid.UUID) (*contracts.Content, error) {
	return f.Service.Archive(f.Ctx, f.Tx, row)
}

// refuse is an operation's call as a refusal reads it: the error alone, because
// what a refused call left behind is the harness's assertion and not the port's.
func refuse(call func(Fixture, uuid.UUID) (string, error)) func(Fixture, uuid.UUID) error {
	return func(f Fixture, row uuid.UUID) error {
		_, err := call(f, row)
		return err
	}
}

// answer is what a command handed back, rendered so that two answers compare
// with ==. The retry reads it either side of the second call: a service that
// stores the right row and answers a publication time an hour later has handed
// its caller a row nothing wrote, and no snapshot of the store can see that.
// "Publishing twice does not move the publication time" is a sentence about the
// answer as much as about the row, which is why the time is in here.
func answer(c *contracts.Content) string {
	if c == nil {
		return "nothing"
	}
	return fmt.Sprintf("id=%s slug=%q status=%s published=%s", c.ID, c.Slug, c.Status, at(c.PublishedAt))
}

// archive files content away and fails the case if it could not, for the steps
// that only need the state and not the answer.
func archive(t *testing.T, f Fixture, row uuid.UUID) *contracts.Content {
	t.Helper()
	got, err := archiveErr(f, row)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	return got
}

// snapshot is everything the three commands can write, rendered so that two
// readings compare with ==. The publication time is in it because a retry that
// moved it would be a page published twice, and the update time because a
// refused call that stamped a row wrote to it.
func snapshot(t *testing.T, f Fixture, row uuid.UUID) string {
	t.Helper()
	c, err := f.Content(row)
	if err != nil {
		return "no such content"
	}
	return fmt.Sprintf("slug=%q title=%q body=%q kind=%s status=%s published=%s updated=%s",
		c.Slug, c.Title, c.Body, c.Kind, c.Status, at(c.PublishedAt), at(&c.UpdatedAt))
}

func at(when *time.Time) string {
	if when == nil {
		return "never"
	}
	return when.UTC().Format(time.RFC3339Nano)
}

// draft is content in the state everything starts in.
func draft(slug string) *contracts.Content {
	return &contracts.Content{Slug: slug, Title: "About us", Body: "# About\n\nWe make things.", Kind: contracts.KindPage}
}

// live seeds content and publishes it, which is two steps everywhere.
func live(t *testing.T, f Fixture, slug string) *contracts.Content {
	t.Helper()
	out, err := f.Service.Publish(f.Ctx, f.Tx, f.Seed(draft(slug)))
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return out
}

// cases is what the description cannot express, each with the reason it is
// written by hand.
func cases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "publishing serves it and records when",
			Because: "what a success leaves behind is the port's domain: published means published, at a time somebody can print",
			Run: func(t *testing.T, f Fixture) {
				got := live(t, f, "about-us")
				if got.Status != contracts.StatusPublished {
					t.Errorf("status is %q, want %q", got.Status, contracts.StatusPublished)
				}
				if got.PublishedAt == nil {
					t.Error("published content has no publication time")
				}
			},
		},
		{
			Name:    "unpublishing clears the publication time",
			Because: "domain contents: published means published at a time, and this is the half that puts both away together",
			Run: func(t *testing.T, f Fixture) {
				back, err := f.Service.Unpublish(f.Ctx, f.Tx, live(t, f, "about-us").ID)
				if err != nil {
					t.Fatalf("Unpublish: %v", err)
				}
				if back.Status != contracts.StatusDraft || back.PublishedAt != nil {
					t.Errorf("it is %q/%v; published means published at a time, and this is not published",
						back.Status, back.PublishedAt)
				}
			},
		},
		{
			Name:    "unpublishing takes content out of the archive",
			Because: "the description gives an operation one Ready, and Unpublish has two starting states: coming back from the archive is the second, and it publishes the same event",
			Run: func(t *testing.T, f Fixture) {
				id := f.Seed(draft("about-us"))
				archive(t, f, id)
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
		},
		{
			Name:    "archiving keeps it and serves it to nobody",
			Because: "domain contents: the archive is a status and not a delete, and an archived page is not published either",
			Run: func(t *testing.T, f Fixture) {
				filed := archive(t, f, live(t, f, "about-us").ID)
				if filed.Status != contracts.StatusArchived || filed.PublishedAt != nil {
					t.Errorf("it is %q/%v, want it archived and not published", filed.Status, filed.PublishedAt)
				}
			},
		},
		{
			Name:    "only published content is served publicly",
			Because: "the read answered across three states of one row — draft, published, archived — and the description gives Public one Ready",
			Run: func(t *testing.T, f Fixture) {
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
				archive(t, f, id)
				if _, err := f.Service.Public(f.Ctx, f.Tx, "about-us"); !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("an archived page is served publicly = %v, want ErrNotFound", err)
				}
			},
		},
		{
			Name:    "a slug is stored and looked up the same way",
			Because: "domain: the name was written with capitals and spaces, and the URL that reaches it is the normalised one, which no generated case knows about",
			Run: func(t *testing.T, f Fixture) {
				got := live(t, f, "About Us!")
				if got.Slug != "about-us" {
					t.Errorf("the slug is %q, want %q", got.Slug, "about-us")
				}
				if _, err := f.Service.Public(f.Ctx, f.Tx, "About  Us"); err != nil {
					t.Errorf("looking up %q: %v; a name stored one way and looked up another is a page nobody can reach", "About  Us", err)
				}
			},
		},
	}
}
