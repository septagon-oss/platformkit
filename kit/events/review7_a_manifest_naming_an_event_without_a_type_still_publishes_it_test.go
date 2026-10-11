package events_test

// A reviewer's pin (review round 7, task T-0109). Nothing here fails at the head
// it was written against: it holds two sentences this branch rests on — one about
// the names a consumer compiles against, one about what naming an event does and
// does not oblige at the outbox.
//
// 1. The names. This branch changed what a consumer compiles against:
// `Module.Events` gained a typed sibling (`Module.Declared`), `events.JetStream`
// and `events.Memory` left, and `rest.Spec` gained `Declared()` beside its
// `Events()`. What a consumer actually reads was measured over the client tree
// that consumes this module rather than guessed:
//
//	$ git -C <clients> grep -ho 'events\.[A-Z][A-Za-z0-9_]*' origin/main -- '*.go' | sed 's/^events\.//' | sort | uniq -c | sort -rn
//	     56 Publish     9 Subscription    9 Event    1 Transport
//	$ git -C <clients> grep -n 'events\.JetStream\|ConnectJetStream\|events\.Memory(' origin/main -- '*.go'
//	(no lines: no consumer calls the two constructors this branch removed)
//
// Those names are the whole surface a consumer of this package builds against,
// and this file is the compile-time half of that sentence: the release that
// removes one should turn `make check` red here, instead of turning a tree of
// consumers red on the night it lands.
//
// 2. The obligation. A manifest may name an event without naming its payload
// type — `Module.Events` is still `[]string` for exactly that reason — and
// `Module.Emits` spells such a name as `events.Declared{Name: …}` with a nil
// `Payload`, which `Declared.Schema` answers with a nil schema and the outbox's
// `checkPayload` with no check. So the payload of every event named by a manifest
// in the bare spelling is, today, unchecked at the door. Both directions of that
// are load-bearing and neither is a default: start checking bare names and every
// consumer's emission stops at the outbox the day this version reaches them;
// stop checking typed ones and `event_schema_coverage` becomes a number no door
// holds. The two first cases below publish one identical body under one identical
// name, the only difference being whether the manifest described it.

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// v1Names are the identifiers of this package a consumer compiles against, at
// the counts measured above. They are named, not exercised: the claim under test
// is about the source a consumer builds, and a build is exactly what would break.
var (
	_ events.Event                                               = events.Event{}
	_ events.Handler                                             = func(context.Context, db.Tx[db.Tenant], events.Event) error { return nil }
	_ events.Subscription                                        = events.Subscription{Name: "note.written"}
	_ func(string) bool                                          = events.ValidName
	_ func(context.Context, db.Tx[db.Tenant], string, any) error = events.Publish // 56 call sites
	_ func(*testing.T, ...events.Declared)                       = declare        // the catalog's one writer
	_ func(events.Declared) string                               = func(d events.Declared) string { return d.Name }
)

// undescribedBody is one JSON document that is not a projection of invoiceIssued:
// its only member is one no field of that type has. It is the shape of a
// hand-built payload, which is why no schema may be inferred for it.
var undescribedBody = map[string]any{"anything": 1}

// publishIn writes body under name as the given tenant, through the one door
// every event leaves by.
func publishIn(t *testing.T, conn *db.Conn, who tenancy.Tenant, name string, body any) error {
	t.Helper()
	return db.Run(tenancy.WithTenant(t.Context(), who), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, name, body)
		})
}

// outboxRows counts what the door left behind for one name and one tenant. It
// reads the row's own key, so it answers the same way whatever words a refusal
// happens to print — the probe never depends on the message it is checking.
func outboxRows(t *testing.T, admin *sql.DB, name string, who tenancy.Tenant) int {
	t.Helper()
	var n int
	if err := admin.QueryRow(
		`SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2`,
		name, who.ID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestAManifestNamingAnEventWithoutATypeStillPublishesIt(t *testing.T) {
	const name = "note.written"

	t.Run("named, undescribed: the payload leaves", func(t *testing.T) {
		admin, conn := dbtest.Schema(t)
		// Exactly what Module.Emits writes for a manifest whose Events list is
		// []string{name} — the only spelling any consumer of this module has.
		declare(t, events.Declared{Name: name})

		if err := publishIn(t, conn, acme, name, undescribedBody); err != nil {
			t.Fatalf("the outbox refused a payload nothing described: %v", err)
		}
		if got := outboxRows(t, admin, name, acme); got != 1 {
			t.Errorf("the outbox holds %d rows for %s: the accepted publish wrote none the tenant can be shown", got, name)
		}
	})

	t.Run("named, and its type named: the same payload is refused", func(t *testing.T) {
		admin, conn := dbtest.Schema(t)
		declare(t, events.Declare[invoiceIssued](name))

		err := publishIn(t, conn, acme, name, undescribedBody)
		if err == nil {
			t.Fatal("the outbox accepted a payload that is not the declared type; the coverage number now describes no door")
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the refusal does not name the event: %v", err)
		}
		if got := outboxRows(t, admin, name, acme); got != 0 {
			t.Errorf("a refused publish left %d rows for %s; a refused mutation writes nothing", got, name)
		}
	})

	t.Run("no catalog installed: nothing is checked either", func(t *testing.T) {
		admin, conn := dbtest.Schema(t)
		// An application that owns its own persistence composes the parts
		// directly and never calls DeclareAll (kit/events/README.md, "import the
		// parts directly"): the door then stays open rather than refusing every
		// event for a shape it was never told about.
		if err := publishIn(t, conn, acme, name, undescribedBody); err != nil {
			t.Fatalf("the outbox refused every event in a process that declared none: %v", err)
		}
		if got := outboxRows(t, admin, name, acme); got != 1 {
			t.Errorf("the outbox holds %d rows for %s", got, name)
		}
	})
}

// TestTheCatalogIsOneProcessCheck, NotPerTenant is the other side of the same
// door: DeclareAll is a process fact, so two tenants publishing the same declared
// name in one process get one verdict, and no tenant's publisher can widen the
// check another tenant's publisher meets (decision 0028's shared instance).
func TestTheCatalogIsOneProcessCheckNotPerTenant(t *testing.T) {
	const name = "billing.invoice_issued"
	admin, conn := dbtest.Schema(t)
	declare(t, events.Declare[invoiceIssued](name))

	other := tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"}
	if err := publishIn(t, conn, other, name, undescribedBody); err == nil {
		t.Errorf("tenant %s was let through a check the first tenant's publisher failed", other.Slug)
	}
	if err := publishIn(t, conn, acme, name, undescribedBody); err == nil {
		t.Error("the first tenant's publisher was let through a check it failed a moment ago")
	}
	for _, who := range []tenancy.Tenant{acme, other} {
		if got := outboxRows(t, admin, name, who); got != 0 {
			t.Errorf("%s: two refused publishes left %d rows for %s", who.Slug, got, name)
		}
	}
}
