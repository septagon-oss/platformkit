package porttest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// rowBox is a row in the shape a module's real row has: fields behind a header, a
// field the type keeps to itself, and the identifiers and timestamps a copy written
// in reflect must not lose on the way through.
type rowBox struct {
	id       uuid.UUID
	When     time.Time
	Count    int64
	Title    string
	Tags     []string
	Meta     map[string]string
	Owner    *string
	Sections []rowSection
	Nil      []string
}

type rowSection struct {
	Heading string
	Words   []string
}

func aRow() rowBox {
	return rowBox{
		id:    uuid.New(),
		When:  time.Unix(1700000000, 0).UTC(),
		Count: 3,
		Title: "keep",
		Tags:  []string{"one", "two"},
		Meta:  map[string]string{"owner": "here"},
		// A header the walk has to follow one level further than a slice: the
		// struct a pointer names holds its own slice.
		Owner:    ptrTo("someone"),
		Sections: []rowSection{{Heading: "Body", Words: []string{"first"}}},
	}
}

func ptrTo(s string) *string { return &s }

func anotherTenant(t *testing.T) context.Context {
	t.Helper()
	return tenancy.WithTenant(context.Background(), tenancy.Tenant{ID: uuid.New(), Slug: "there"})
}

// TestTheRowAStoreHandsOutSharesNoStorageWithTheRowItHolds is the store's side of
// `detached`, in both directions: a fixture that goes on editing the row it seeded
// moves no row the store holds, and a command that edits the row it read cannot
// reach the stored one behind a slice, a map or a pointer.
func TestTheRowAStoreHandsOutSharesNoStorageWithTheRowItHolds(t *testing.T) {
	ctx := oneTenantContext()
	store := NewStore(func(r rowBox) uuid.UUID { return r.id })
	seeded := aRow()
	store.Put(ctx, seeded)

	seeded.Tags[0] = "moved"
	seeded.Meta["owner"] = "moved"
	*seeded.Owner = "moved"
	seeded.Sections[0].Words[0] = "moved"

	got, err := store.Get(ctx, seeded.id)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	for _, wrong := range []struct {
		what, was, is string
	}{
		{"Tags[0]", "one", got.Tags[0]},
		{`Meta["owner"]`, "here", got.Meta["owner"]},
		{"*Owner", "someone", *got.Owner},
		{"Sections[0].Words[0]", "first", got.Sections[0].Words[0]},
	} {
		if wrong.is != wrong.was {
			t.Errorf("the stored %s became %q when the fixture edited the copy it seeded; %q was written through a header the store shares with the caller", wrong.what, wrong.is, wrong.was)
		}
	}

	got.Tags[1] = "moved"
	got.Meta["owner"] = "moved"
	*got.Owner = "moved"
	got.Sections[0].Words[0] = "moved"
	again, err := store.Get(ctx, seeded.id)
	if err != nil {
		t.Fatalf("reading the row a second time: %v", err)
	}
	if again.Tags[1] != "two" || again.Meta["owner"] != "here" || *again.Owner != "someone" || again.Sections[0].Words[0] != "first" {
		t.Errorf("the stored row became %v %v %q %v after a reader edited the copy Get handed it; the row a command is handed is its own, whatever it writes into it",
			again.Tags, again.Meta, *again.Owner, again.Sections[0].Words)
	}
	if again.Title != "keep" || again.Count != 3 || again.id != seeded.id || !again.When.Equal(seeded.When) {
		t.Errorf("the copy lost what it should have kept: %+v", again)
	}
}

// TestDetachedKeepsWhatWasNilNilAndWhatTheRowDoesNotExport is the two ways a copy
// written in reflect goes wrong quietly: turning a nil slice into an empty one,
// which a module's Snapshot renders differently and a case then fails for the
// copy's own change, and losing a field reflect cannot write, which is where a
// row's id and its timestamps live.
func TestDetachedKeepsWhatWasNilNilAndWhatTheRowDoesNotExport(t *testing.T) {
	row := aRow()
	row.Tags, row.Meta, row.Owner, row.Sections = nil, nil, nil, nil
	copied := detached(row)
	if copied.Tags != nil || copied.Meta != nil || copied.Owner != nil || copied.Sections != nil {
		t.Errorf("detached turned a nil field into an empty one: %#v", copied)
	}
	if copied.id != row.id || !copied.When.Equal(row.When) || copied.Count != row.Count || copied.Title != row.Title {
		t.Errorf("detached lost what the row is made of: %+v of %+v", copied, row)
	}
}

// TestADoThatWroteThroughAHeaderKeepsWhatItCommitted is the other half of the cure
// review 10's witness does not cover: the copy has to protect the stored row without
// losing the write. A command that succeeded has to leave the row it wrote — the
// slice element, the map key and the struct behind a pointer included — and the row
// it hands back has to be the caller's own afterwards.
func TestADoThatWroteThroughAHeaderKeepsWhatItCommitted(t *testing.T) {
	ctx := oneTenantContext()
	fake := NewFake(time.Unix(1700000000, 0).UTC(), []string{"row.tagged"})
	store := NewStore(func(r rowBox) uuid.UUID { return r.id })
	before := aRow()
	store.Put(ctx, before)

	after, err := Do(ctx, fake, store, Command[rowBox]{
		Row: before.id,
		Apply: func(r *rowBox) []string {
			r.Tags[0] = "rewritten"
			r.Meta["owner"] = "rewritten"
			*r.Owner = "rewritten"
			r.Sections[0].Words[0] = "rewritten"
			r.Sections = append(r.Sections, rowSection{Heading: "More", Words: []string{"second"}})
			return []string{"row.tagged"}
		},
	})
	if err != nil {
		t.Fatalf("a command whose event the port declares has to commit: %v", err)
	}
	stored, err := store.Get(ctx, before.id)
	if err != nil {
		t.Fatalf("reading the committed row: %v", err)
	}
	if stored.Tags[0] != "rewritten" || stored.Meta["owner"] != "rewritten" || *stored.Owner != "rewritten" ||
		stored.Sections[0].Words[0] != "rewritten" || len(stored.Sections) != 2 || stored.Sections[1].Words[0] != "second" {
		t.Errorf("the committed row reads %v %v %q %v; the copy that protects a refused command must not swallow a command that succeeded",
			stored.Tags, stored.Meta, *stored.Owner, stored.Sections)
	}
	if stored.Nil != nil {
		t.Errorf("the row that came back carries %v where it had none", stored.Nil)
	}
	after.Tags[0] = "moved afterwards"
	again, err := store.Get(ctx, before.id)
	if err != nil {
		t.Fatalf("reading the row after the caller kept editing its answer: %v", err)
	}
	if again.Tags[0] != "rewritten" {
		t.Errorf("the stored row followed the caller's copy to %q; the answer a command hands back is the caller's, not another way into the tenant's row", again.Tags[0])
	}
	if said := fake.Events.Names(); len(said) != 1 || said[0] != "row.tagged" {
		t.Errorf("the committed command said %v", said)
	}
}
