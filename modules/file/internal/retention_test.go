package internal_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// sweepWalk is the jobs.TenantLister the retention sweep is handed at
// composition — tenant.Active in a real application — with the answer written
// down: which tenants this run walks. It is a fake because the tenant registry
// is another module's table; what is under test is what the sweep does with the
// list, and every row it touches it touches from a transaction the real
// jobs.PerTenant mints for one tenant.
type sweepWalk struct{ walking []tenancy.Tenant }

func (w sweepWalk) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) {
	return w.walking, nil
}

// keepInvoices is the policy this deployment "configures": a class and how long
// it lives. The module names no class and interprets no duration — the sweep
// matches one token against the other — so any pair of names would do, and the
// one that reads like an invoice is the one a reader will recognise as a
// product's word rather than the module's.
const keepInvoices = 30 * 24 * time.Hour

// TestTheRetentionSweepRemovesWhatItsPolicyCovers is the cron path — the only
// door into the retention policy that no command and no route opens — read
// against rows.
//
// Four rules decide what disappears and the case walks all four: a class with a
// policy past its cutoff goes, a class with no policy is kept however old it is
// (refuse the write that takes the last copy away, never the write that finds
// none), a class inside its window is kept, and a live hold outranks a class
// that ran out — while a hold whose clock has itself run out outranks nothing,
// which is the half a hold row that only looks alive would get wrong.
//
// Two tenants are listed and a third is deliberately not. The sweep's query
// carries no tenant predicate of its own — row-level security is what bounds it
// — so the unlisted tenant's expired file surviving is the tenancy claim being
// tested rather than restated.
//
// And it is the job that starts at the rows, so it touches no bytes: every blob
// is still where it was when the sweep finishes, and the three that go, go
// through the subscription over the events this run published — which the case
// then runs, twice, to the certificate.
func TestTheRetentionSweepRemovesWhatItsPolicyCovers(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0, 0)

	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	// inertia is never listed to the sweep, which is the case for listing it.
	inertia := tenancy.Tenant{ID: uuid.New(), Slug: "inertia", Name: "Inertia"}

	// One seeded file: its class, how long ago it arrived, and a hold up to
	// when, given as a duration from now so that a negative one is spent.
	seed := []struct {
		tenant tenancy.Tenant
		name   string
		kind   string
		ago    time.Duration
		held   time.Duration
	}{
		{acme, "invoice-old.txt", "invoice", 40 * 24 * time.Hour, 0},
		{acme, "invoice-new.txt", "invoice", 0, 0},
		{acme, "photo-old.txt", "photo", 400 * 24 * time.Hour, 0},
		{acme, "invoice-held.txt", "invoice", 40 * 24 * time.Hour, time.Hour},
		{acme, "invoice-spent-hold.txt", "invoice", 40 * 24 * time.Hour, -time.Hour},
		{globex, "invoice-old.txt", "invoice", 40 * 24 * time.Hour, 0},
		{inertia, "invoice-old.txt", "invoice", 40 * 24 * time.Hour, 0},
	}
	ids := map[string]uuid.UUID{}
	for _, k := range seed {
		key := k.tenant.Slug + "/" + k.name
		err := db.Run(tenancy.WithTenant(t.Context(), k.tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			f, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: k.name, ContentType: "text/plain", Declared: -1,
				Kind: k.kind, Body: strings.NewReader(k.name),
			})
			if err != nil {
				return err
			}
			// The two things the sweep reads are db.Now() and the row's own
			// created_at, so the age is part of the seed: nothing here waits an
			// hour, and nothing swaps in a clock production would not consult.
			if k.ago > 0 {
				if err := tx.DB().Exec(`UPDATE files SET created_at = ? WHERE id = ?`,
					db.Now().Add(-k.ago), f.ID).Error; err != nil {
					return err
				}
			}
			if k.held != 0 {
				until := db.Now().Add(time.Hour)
				if _, err := svc.Retain(ctx, tx, f.ID, &until, "litigation hold"); err != nil {
					return err
				}
				if k.held < 0 {
					// Placed live and since spent. Retain refuses a hold that is
					// born expired, so the clock runs out the only way it can in
					// production: between one tick and the next. Both columns move,
					// because the table refuses a hold that expires before it was
					// placed.
					if err := tx.DB().Exec(`UPDATE file_holds SET until = ?, created_at = ? WHERE file_id = ?`,
						db.Now().Add(k.held), db.Now().Add(2*k.held), f.ID).Error; err != nil {
						return err
					}
				}
			}
			ids[key] = f.ID
			return nil
		})
		if err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	if len(ids) != len(seed) {
		t.Fatalf("the seed wrote %d files, want %d", len(ids), len(seed))
	}
	before := keysUnder(t, dir)

	// A policy nobody configured is no job, and neither is a policy with no way
	// to walk the tenants: file.Module panics at the second, and schedules
	// nothing for either, because a retention promise that quietly does not run
	// is the failure nobody sees.
	if scheduled := internal.NewSweep(store, internal.SweepConfig{Retention: nil, Tenants: sweepWalk{}}).Jobs(); scheduled != nil {
		t.Errorf("a deployment with no policy schedules %v, want nothing", scheduled)
	}
	if scheduled := internal.NewSweep(store, internal.SweepConfig{Retention: map[string]time.Duration{"invoice": keepInvoices}}).Jobs(); scheduled != nil {
		t.Errorf("a policy with no tenant lister schedules %v, want nothing", scheduled)
	}

	sweep := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{"invoice": keepInvoices},
		Tenants:   sweepWalk{[]tenancy.Tenant{acme, globex}},
	})
	scheduled := sweep.Jobs()
	if len(scheduled) != 1 || scheduled[0].Name != "file-retention" || scheduled[0].Cron != "15 4 * * *" {
		t.Fatalf("the module schedules %v, want the daily file-retention job", scheduled)
	}
	if err := scheduled[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention sweep: %v", err)
	}

	// One query for all seven files, so the policy's four rules are judged the
	// same way: present or not, on the row, after the job ran.
	for key, want := range map[string]bool{
		"acme/invoice-old.txt":        false, // its class ran out
		"acme/invoice-new.txt":        true,  // inside the window
		"acme/photo-old.txt":          true,  // no policy for its class
		"acme/invoice-held.txt":       true,  // a live hold outranks the class
		"acme/invoice-spent-hold.txt": false, // the hold's own clock ran out
		"globex/invoice-old.txt":      false, // the walk reached this tenant too
		"inertia/invoice-old.txt":     true,  // never listed, so never even seen
	} {
		var rows int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM files WHERE id = $1 AND deleted_at IS NULL`, ids[key]).Scan(&rows); err != nil {
			t.Fatalf("read %s back: %v", key, err)
		}
		if got := rows > 0; got != want {
			t.Errorf("after the sweep %s present = %v, want %v", key, got, want)
		}
	}

	// Every removal is one file.deleted naming its cause, and nothing else went:
	// the row's removal is the event's reason, and an expired file that vanished
	// without one could never be certified as erased.
	var expired, beside, filed int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FILTER (WHERE payload->>'cause' = $2),
		        count(*) FILTER (WHERE payload->>'cause' IS DISTINCT FROM $2),
		        count(DISTINCT tenant_id)
		   FROM `+outbox+` WHERE name = $1`, contracts.EventDeleted, contracts.EraseExpired).
		Scan(&expired, &beside, &filed); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if expired != 3 || beside != 0 {
		t.Errorf("the sweep published %d removals for their class running out and %d others, want three of its own", expired, beside)
	}
	if filed != 2 {
		t.Errorf("the removals are filed under %d tenants, want the two the sweep was told to walk", filed)
	}

	// It started at the rows, so it never reached the store.
	if got := len(keysUnder(t, dir)); got != len(before) {
		t.Errorf("the sweep removed bytes: %d blobs left, want the %d it started with", got, len(before))
	}

	// A second tick finds nothing left to do and writes nothing while finding it
	// out, or the outbox would fill with a removal every tenant already made.
	if err := scheduled[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention sweep's second tick: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+outbox+` WHERE name = $1`, contracts.EventDeleted).Scan(&expired); err != nil {
		t.Fatalf("read the outbox again: %v", err)
	}
	if expired != 3 {
		t.Errorf("the second tick left %d removals in the outbox, want the three the first one published", expired)
	}

	// And the chain the sweep starts ends where every removal ends: the
	// subscription takes the bytes for each event and certifies it — which is
	// the only thing that turns a row that stopped existing into a store that
	// stopped holding it. Twice, because the transport promises at-least-once.
	sub := internal.EraseBlobs(store)
	for pass := range 2 {
		for _, tenant := range []tenancy.Tenant{acme, globex} {
			if err := deliverEach(t.Context(), conn, tenant, sub); err != nil {
				t.Fatalf("delivery round %d for %s: %v", pass, tenant.Slug, err)
			}
		}
	}
	if got, want := len(keysUnder(t, dir)), len(before)-3; got != want {
		t.Errorf("after the subscription %d blobs are left, want %d: the four the policy kept and the three the sweep never reached",
			got, want)
	}
	var proofs, certified int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*), count(*) FILTER (WHERE verified_at IS NOT NULL) FROM file_erasures WHERE cause = $1`,
		contracts.EraseExpired).Scan(&proofs, &certified); err != nil {
		t.Fatalf("read the proofs: %v", err)
	}
	if proofs != 3 || certified != 3 {
		t.Errorf("the sweep's removals left %d certificates, %d of them verified; want three and three", proofs, certified)
	}
}

// deliverEach hands every removal this tenant published to the subscription,
// in that tenant's own transaction, as the worker would.
func deliverEach(ctx context.Context, conn *db.Conn, tenant tenancy.Tenant, sub events.Subscription) error {
	var pending []json.RawMessage
	err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var payloads string
		if err := tx.DB().Raw(`SELECT coalesce(json_agg(payload ORDER BY id), '[]'::json)::text FROM `+
			outbox+` WHERE name = ?`, contracts.EventDeleted).Scan(&payloads).Error; err != nil {
			return err
		}
		return json.Unmarshal([]byte(payloads), &pending)
	})
	if err != nil {
		return err
	}
	for _, payload := range pending {
		err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return sub.Handler(ctx, tx, events.Event{
				Name: contracts.EventDeleted, TenantID: tenant.ID, Payload: payload,
			})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// TestTheRetentionSweepTakesOneBatchATick is the limit the job's own comment
// promises: one tenant's tick removes sweepBatch files and no more, and the next
// tick takes the rest.
//
// A sweep with no ceiling holds a database connection across the whole of a
// tenant's history — a decade of invoices, one tick, one lock the operator can
// watch — and the retry that a long tick risks is a job that never finishes
// before the next one starts. The cost of the ceiling is that a policy that
// changes shape empties gradually rather than at once, and this case pins both
// halves: two hundred go on the first tick, the two-hundred-and-first on the
// second, and nothing between them is left half-done.
func TestTheRetentionSweepTakesOneBatchATick(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0, 0)

	const files = 201
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for n := range files {
			f, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: fmt.Sprintf("invoice-%03d.txt", n), ContentType: "text/plain", Declared: -1,
				Kind: "invoice", Body: strings.NewReader("body"),
			})
			if err != nil {
				return fmt.Errorf("upload %d: %w", n, err)
			}
			_ = f
		}
		// One statement, because the age is the seed and not the point: every
		// file here is past its class's window before the job starts.
		return tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '40 days'`).Error
	})
	if err != nil {
		t.Fatalf("seed %d files: %v", files, err)
	}

	sweep := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{"invoice": keepInvoices},
		Tenants:   sweepWalk{[]tenancy.Tenant{acme}},
	})
	scheduled := sweep.Jobs()
	if len(scheduled) != 1 {
		t.Fatalf("the module schedules %v", scheduled)
	}

	present := func() int {
		t.Helper()
		var rows int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM files WHERE deleted_at IS NULL`).Scan(&rows); err != nil {
			t.Fatalf("count what is left: %v", err)
		}
		return rows
	}

	if err := scheduled[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the first tick: %v", err)
	}
	if got := present(); got != files-200 {
		t.Errorf("the first tick left %d files, want the %d over its batch", got, files-200)
	}
	if err := scheduled[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the second tick: %v", err)
	}
	if got := present(); got != 0 {
		t.Errorf("the second tick left %d files, want none", got)
	}
}
