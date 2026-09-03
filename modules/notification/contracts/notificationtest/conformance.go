package notificationtest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a way to see what was published.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Published is the names of the events published so far, in order. It is
	// what holds MarkRead to saying nothing the second time, which is the half
	// of the promise a return value cannot show.
	Published func() []string
}

// silent runs step and fails if it published anything.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; repeating a command changes nothing, so it says nothing", what, after[before:])
	}
}

// Harness builds one Fixture and calls run with it. It is written this way
// round because the real service's fixture is a transaction, and a transaction
// is a scope somebody has to close.
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

// Ada is the recipient the suite writes to, and Bob is somebody else. They are
// exported because a harness has to tell the RecipientLookup which of them has
// an address: Ada does, Bob does not, which is what makes "a recipient with no
// address gets the row and no mail" a case.
var (
	Ada      = uuid.New()
	Bob      = uuid.New()
	AdaEmail = "ada@acme.example.com"
)

// notice is one notice for Ada.
func notice(title string) contracts.Notice {
	return contracts.Notice{Recipient: Ada, Title: title, Body: "the details", Link: "/admin/task/tasks/1"}
}

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"telling somebody writes a row and says so": func(t *testing.T, f Fixture) {
			got, err := f.Service.Notify(f.Ctx, f.Tx, notice("  A task was assigned to you  "))
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			switch {
			case got.RecipientID != Ada:
				t.Errorf("the notice went to %s, want %s", got.RecipientID, Ada)
			case got.Title != "A task was assigned to you":
				t.Errorf("title is %q, want it trimmed", got.Title)
			case got.ReadAt != nil:
				t.Error("a new notification is already read")
			}
			published(t, f, contracts.EventCreated)
		},

		"a notice needs somebody to tell and something to say": func(t *testing.T, f Fixture) {
			for what, n := range map[string]contracts.Notice{
				"nobody":           {Title: "hello"},
				"nothing":          {Recipient: Ada},
				"an absolute link": {Recipient: Ada, Title: "hello", Link: "https://elsewhere.example.com"},
			} {
				if _, err := f.Service.Notify(f.Ctx, f.Tx, n); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Notify with %s = %v, want ErrInvalid", what, err)
				}
			}
		},

		"asking for mail asks the worker for it": func(t *testing.T, f Fixture) {
			n := notice("A task was assigned to you")
			n.Email = true
			if _, err := f.Service.Notify(f.Ctx, f.Tx, n); err != nil {
				t.Fatalf("Notify: %v", err)
			}
			// Two events and no mail server: the send happens in the worker,
			// so a request never waits on somebody else's machine.
			published(t, f, contracts.EventCreated, contracts.EventEmailRequested)
		},

		"a recipient with no address gets the row and no mail": func(t *testing.T, f Fixture) {
			n := notice("A task was assigned to you")
			n.Recipient, n.Email = Bob, true
			got, err := f.Service.Notify(f.Ctx, f.Tx, n)
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			if got.RecipientID != Bob {
				t.Errorf("the notice went to %s, want %s", got.RecipientID, Bob)
			}
			// Refusing the whole call would mean somebody with no address
			// could not be told anything.
			published(t, f, contracts.EventCreated)
		},

		"marking one read says so once": func(t *testing.T, f Fixture) {
			got, err := f.Service.Notify(f.Ctx, f.Tx, notice("A task was assigned to you"))
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			read, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Ada)
			if err != nil {
				t.Fatalf("MarkRead: %v", err)
			}
			if read.ReadAt == nil {
				t.Error("a notification marked read has no time on it")
			}
			f.silent(t, "MarkRead again", func() {
				if _, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Ada); err != nil {
					t.Fatalf("MarkRead again: %v", err)
				}
			})
			published(t, f, contracts.EventCreated, contracts.EventRead)
		},

		"somebody else's notification is not found": func(t *testing.T, f Fixture) {
			got, err := f.Service.Notify(f.Ctx, f.Tx, notice("A task was assigned to you"))
			if err != nil {
				t.Fatalf("Notify: %v", err)
			}
			// Not a 403: the only thing a caller may learn about a
			// notification that is not theirs is that they do not have one
			// with that id.
			if _, err := f.Service.MarkRead(f.Ctx, f.Tx, got.ID, Bob); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("MarkRead of somebody else's notification = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.MarkRead(f.Ctx, f.Tx, uuid.New(), Ada); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("MarkRead of an unknown id = %v, want ErrNotFound", err)
			}
		},

		"a query cannot re-address a list": func(t *testing.T, f Fixture) {
			// The recipient is the caller, resolved from the principal, and it
			// is not a parameter. Both implementations set the filter rather
			// than merging it, so there is no shape of Query that lists
			// somebody else's rows — which is what lets the route be SignedIn
			// with no permission at all. An implementation that honoured the
			// caller's filter would turn one signed-in person into a reader of
			// everybody's notifications, and it would look like paging.
			if _, err := f.Service.Notify(f.Ctx, f.Tx, notice("Ada's")); err != nil {
				t.Fatalf("Notify Ada: %v", err)
			}
			hers := notice("Bob's")
			hers.Recipient = Bob
			if _, err := f.Service.Notify(f.Ctx, f.Tx, hers); err != nil {
				t.Fatalf("Notify Bob: %v", err)
			}
			rows, total, err := f.Service.ListFor(f.Ctx, f.Tx, Bob, crud.Query{
				Filter: map[string]any{"recipientId": Ada},
			})
			if err != nil {
				t.Fatalf("ListFor with a filter of the caller's own: %v", err)
			}
			if total != 1 || len(rows) != 1 || rows[0].RecipientID != Bob {
				t.Fatalf("asking for Ada's rows as Bob returned %d of %d; the list is the caller's", len(rows), total)
			}
		},

		"a list is one person's, newest first": func(t *testing.T, f Fixture) {
			for _, title := range []string{"first", "second"} {
				if _, err := f.Service.Notify(f.Ctx, f.Tx, notice(title)); err != nil {
					t.Fatalf("Notify %s: %v", title, err)
				}
			}
			other := notice("not yours")
			other.Recipient = Bob
			if _, err := f.Service.Notify(f.Ctx, f.Tx, other); err != nil {
				t.Fatalf("Notify Bob: %v", err)
			}

			rows, total, err := f.Service.ListFor(f.Ctx, f.Tx, Ada, crud.Query{})
			if err != nil {
				t.Fatalf("ListFor: %v", err)
			}
			if total != 2 || len(rows) != 2 {
				t.Fatalf("Ada's list holds %d of %d rows, want her own two", len(rows), total)
			}
			if rows[0].Title != "second" {
				t.Errorf("the list starts with %q, want the newest first", rows[0].Title)
			}
			for _, row := range rows {
				if row.RecipientID != Ada {
					t.Errorf("Ada's list holds a row addressed to %s", row.RecipientID)
				}
			}
			if rows, _, err = f.Service.ListFor(f.Ctx, f.Tx, Ada, crud.Query{Limit: 1, Offset: 1}); err != nil ||
				len(rows) != 1 || rows[0].Title != "first" {
				t.Errorf("the second page of Ada's list = %v, %v", rows, err)
			}
		},
	}
}

func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}
