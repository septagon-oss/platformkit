package porttest

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Review 10, finding 1 (MEDIUM). `Do` is the kernel unit every fake's single-row command is
// built on, and its promise is written twice — in `Do` and in README's "The names are checked
// before the write, so an undeclared event cannot leave the row written and the event unsaid:
// domain state and what the command says commit together or neither does". The unit test that
// vouches for it, `TestDoRefusesAnUndeclaredEventWithoutWriting`, compares `stored.Filed`, a
// `bool`. A `bool` sits inside the struct `Do` copies, so the copy is what protects it. A
// slice, a map or a pointer field is not copied: its header is copied and the storage behind
// it is the *stored* row's, and `Apply` receives `*T` and writes through that. So the one
// refusal `Do` makes after `Apply` runs — an undeclared event name — refuses the write and
// has already performed it, for the fields that live behind a header.
//
// The assertion that must hold is the one the README makes: after a `Do` that answered an
// error, the tenant's row is the row that was there before, field for field, including the
// fields behind a header. It fails today and passes the moment `Do` keeps an `Apply` from
// reaching the stored row on a path that ends in an error — hand the command a copy that
// shares nothing (which is also what a database row gives a real command), or re-read and put
// only on the success path.
//
// Nothing shipping is wrong today, measured rather than assumed: `porttest.Do` is called by
// two fakes (`tasktest/fake.go`, `contenttest/fake.go`, once each) and both of their rows are
// all value fields; `sitetest` is a per-tenant singleton that keeps its own map and never
// calls `Do`, and it is the one row type in this repository that carries a slice
// (`SiteSettings.Nav`, a `[]NavItem`). So the door has no user yet — and this change is the
// one that invites every fake to move onto `Do`, which is when it gets one. A suite whose
// Snapshot renders the mutated field would also catch it as "a refused call wrote", which is
// what `review7_rowblind_test.go` exists to make impossible.
func TestADoThatRefusesAnUndeclaredEventLeavesTheReferencedFieldsAlone(t *testing.T) {
	ctx := oneTenantContext()
	fake := NewFake(time.Unix(1700000000, 0).UTC(), []string{"row.tagged"})
	store := NewStore(func(r tagged) uuid.UUID { return r.id })

	before := tagged{id: uuid.New(), Title: "keep", Tags: []string{"one", "two"}, Meta: map[string]string{"owner": "acme"}}
	store.Put(ctx, before)

	_, err := Do(ctx, fake, store, Command[tagged]{
		Row: before.id,
		Apply: func(r *tagged) []string {
			// The three ways a real Apply writes a real row: a value field, an element of a
			// slice the stored row also points at, and a key of a map it also points at.
			r.Title = "rewritten"
			r.Tags[0] = "rewritten"
			r.Meta["owner"] = "rewritten"
			return []string{"row.undeclared"} // the refusal Do makes after Apply ran
		},
	})
	// Reachability, through the behaviour the fix keeps rather than through the defect: the
	// refusal's own sentence, which is correct today and correct after the cure.
	if err == nil || !strings.Contains(err.Error(), "not one of the events this port declares") {
		t.Fatalf("Do answered %v; the undeclared event has to be refused here or nothing below means anything", err)
	}

	stored, err := store.Get(ctx, before.id)
	if err != nil {
		t.Fatalf("reading the row back: %v", err)
	}
	if stored.Title != "keep" {
		t.Errorf("the stored Title became %q after a Do that refused; the row was put after all", stored.Title)
	}
	if stored.Tags[0] != "one" {
		t.Errorf("the stored Tags[0] became %q after a Do that refused the command: Apply wrote through the slice header the stored row shares, so the tenant holds a write no event says and no row was committed for; %q",
			stored.Tags[0], "domain state and what the command says commit together or neither does")
	}
	if stored.Meta["owner"] != "acme" {
		t.Errorf("the stored Meta[\"owner\"] became %q after a Do that refused the command, for the same reason behind a map header", stored.Meta["owner"])
	}
	if said := fake.Events.Names(); len(said) != 0 {
		t.Errorf("the refused command also said %v; a refusal is not news", said)
	}
}

// tagged is a row whose fields are not all values — the shape a real module's row has, since
// a settings row carries a navigation and a content row carries its body sections.
type tagged struct {
	id    uuid.UUID
	Title string
	Tags  []string
	Meta  map[string]string
}
