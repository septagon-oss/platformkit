package events

// ledger.go is the move of the two delivery ledgers onto an app's durables.
//
// A deployment that starts naming its app changes the durable every subscription
// answers to: `mod-ev` becomes `collect+mod-ev` (kit/appname.Durable), because the
// durable is the JetStream consumer name, the deliver group its replicas join, and
// half the primary key of platformkit_handled and platformkit_dead_letters. The
// consumer name moves by itself, the first time the subscription is made. The two
// ledgers do not: they still hold their rows under the old key, and a claim under
// a name nothing subscribes to any more is a claim nobody will ever find.
//
// That is the double-handling window decision 0074's second round named, and its
// shape is worth stating exactly, because the obvious shape is not reachable. A
// scoped consumer's filter set still includes the pre-flip addresses
// (appname.Filters), so DeliverAll does redeliver old-address messages to the new
// durable — and transport.AddressMismatch refuses them at the transport, before any
// handler runs (providers/nats/jetstream.go terminates them with an error log).
// What *is* reachable is a re-publish after the flip: Replay nulls published_at
// (replay.go), the relay carries the row again at the scoped address this build
// publishes at (jetstream.Publish), and the claim the new consumer looks for —
// (event_id, "collect+mod-ev") — is not the row that sits in the table
// ((event_id, "mod-ev")). claim inserts the new key and returns true, and the
// handler runs a second time for work it already committed. The same arrival is
// a consumer remake, which replays from DeliverAll and relies on these rows to
// stop it. Dead letters move for the same reason plus one: claim refuses to write a
// claim while a dead letter exists for that (event_id, durable), so an un-moved
// dead letter leaves the scoped name free to run a handler that was already
// terminated.
//
// The move is a rename and not a copy. A durable row holds no module, no event name
// and no app — only the durable — so the only rename expressible against the table
// is a prefix (kit/appname's own comment on why the scoped durable is the unscoped
// one with "<app>+" in front of it), and a copy would give every app the claims of
// the others, which is an app skipping work it never did. The first app to move
// therefore owns the unscoped history; that is the honest reading of a ledger that
// never recorded which app made a claim, and §Limits of kit/appname already says
// the unscoped deployment is a real deployment rather than a bug.
//
// Why this is not a migration: platformkit_tenant_match answers the empty set to a
// schema file (migrations/README.md, "A file that writes rows"), and the phase=data
// door refuses these two tables by name with the remedy that says what to do
// instead — "a table keyed by something else needs a drain its owner owns, in a
// job" (kit/db/backfill.go). This is that drain: the owning package, its own tables,
// db.RunSystem with a capability whose reason names the act, one transaction, and an
// event per tenant whose ledger moved so the account commits with the rows.

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// EventLedgerMoved names the record that a deployment's delivery ledgers moved
// onto its app's durables. It is the kernel's own event, declared in the
// composition's kernel module list beside EventReplayed, so the audit module —
// which subscribes to every event the composition emits — records the act in the
// trail of each tenant whose ledger moved, with the same actor semantics as the
// envelope's: nobody for a boot, a person for the operator's verb.
const EventLedgerMoved = "platformkit.ledger_moved"

// ledgerToken is the capability the move needs. It is a fifth token rather than a
// reuse of the replay's or the purge's because the reason a cross-tenant
// transaction opens has to be its own — that is what the log line says and what an
// audit of the capability answers — and because borrowing replayToken would borrow
// its demand for an actor, which this act does not make (see the comment on
// LedgerMovedRecord).
var ledgerToken = syscap.NewSystemToken("move a delivery ledger onto its app's durable")

// LedgerMovedRecord is the payload of EventLedgerMoved: which app's names the rows
// took, how many moved, and who asked. It carries no payload and no secret — the
// ledger rows themselves are the record of which events were handled, and the
// outbox still holds whatever bodies survive its purge.
type LedgerMovedRecord struct {
	// App is the scoped prefix, verbatim: the slug the durables now carry.
	App string `json:"app"`
	// Durables names the subscriptions whose ledger moved for this tenant, in
	// their new spelling. One event per tenant rather than one per row keeps the
	// trail readable while the ledger holds a week of deliveries; the names, not
	// a count of rows, is what an operator asks after a deploy.
	Durables []string `json:"durables,omitempty"`
	// Claims is this tenant's handled rows that moved, Dead its terminal ones.
	Claims int64 `json:"claims"`
	Dead   int64 `json:"dead"`
	// RequestedBy says how the move was asked for: "boot" for the worker's own
	// step, the kernel job's name when the scheduler finished what boot could not,
	// or the operator's id for the installation's verb. The distinction is a field
	// rather than an implication because the answers mean different things to a
	// reviewer: a machine re-naming its own ledger, and a person ordering it. An
	// empty value means neither said.
	RequestedBy string `json:"requestedBy,omitempty"`
}

// MoveReport is what one move did, for the log line and the verb's response.
type MoveReport struct {
	// Subscriptions is the number of distinct durables renamed — the count an
	// operator compares with the subscriptions the composition lists.
	Subscriptions int
	// Claims and Dead are rows renamed, summed over tenants; Tenants is how many
	// tenants therefore got a record.
	Claims, Dead, Tenants int64
}

// unscoped is the predicate that says a durable names no app. '+' is appJoin, in
// none of the three grammars an app, a module or an event name is written in
// (kit/appname's argument for the join), and TestADurableCarriesNoDot pins the
// prefix property, so no scoped row can satisfy it and no unscoped row can be
// missed by it. It is the whole idempotence of the move: run twice, the second run
// selects nothing.
const unscoped = "strpos(durable, '+') = 0"

// MoveLedger renames this deployment's handled and dead-letter rows onto the
// durables of app, and records the act in every tenant whose ledger moved.
//
// One transaction, in this order, each step a statement a reviewer can read:
// take the table locks; name the durables that will move; copy each ledger's
// scoped twin; delete the unscoped rows, whose twin the copy just provably made;
// and publish one record per tenant. A refusal writes nothing and emits nothing.
func MoveLedger(ctx context.Context, conn *db.Conn, app appname.Name, requestedBy string) (MoveReport, error) {
	// An app-less deployment has no scoped name to move to, so this is an answer
	// rather than a refusal: nothing moves, nothing is emitted, and the ledgers
	// stay exactly as some other app's consumer will find them.
	if !app.Named() {
		return MoveReport{}, nil
	}
	// The type is not the check: a Name built by conversion bypasses Parse, and a
	// slug a subject token cannot hold forms the *unscoped* durable — the same
	// argument transport.AddressMismatch makes before a delivery, and the reason
	// Consume refuses such a subscription at boot. Moving a ledger under a prefix
	// no consumer could ever ask for would empty the ledgers for the app that does
	// hold them, which is the write that takes the last copy of a claim away.
	if !app.Valid() {
		return MoveReport{}, fmt.Errorf("events: move the delivery ledger for app %q: %s", string(app), notASlug)
	}
	prefix := appname.DurablePrefix(app)
	var report MoveReport
	err := db.RunSystem(ctx, conn, ledgerToken, func(ctx context.Context, tx db.Tx[db.System]) error {
		// The locks first, and NOWAIT. SHARE ROW EXCLUSIVE conflicts with the
		// ROW SHARE an INSERT takes and holds to the end of its transaction, so no
		// claim can be written or cleared while the move runs; plain reads — the
		// purge's, an operator's — are untouched. Without it, the delete below
		// could remove a claim a delivery in another open transaction is about to
		// commit, which is the one way this move could cause a double-handling
		// instead of curing one. NOWAIT is what makes the contention a refusal
		// rather than a queue: a delivery is in flight, nothing moved, run it
		// again. Both tables, always in this order, so two moves cannot deadlock
		// each other.
		if err := lockLedger(ctx, tx, handled); err != nil {
			return err
		}
		if err := lockLedger(ctx, tx, deadLetters); err != nil {
			return err
		}
		// The names that will move, read once and used twice: for the record, and
		// for the report. Under the lock nothing else can add one.
		var durables []string
		if err := tx.DB().Raw(`SELECT DISTINCT durable FROM ` + handled + ` WHERE ` + unscoped + `
				UNION SELECT DISTINCT durable FROM ` + deadLetters + ` WHERE ` + unscoped + `
				ORDER BY durable`).Scan(&durables).Error; err != nil {
			return fmt.Errorf("events: move the delivery ledger for app %s: name the durables: %w", app, err)
		}
		report.Subscriptions = len(durables)
		for i, d := range durables {
			durables[i] = prefix + d
		}
		var (
			claims map[uuid.UUID]int64
			dead   map[uuid.UUID]int64
			err    error
		)
		if claims, err = renameLedger(tx, handled, prefix, []string{"handled_at"}); err != nil {
			return err
		}
		if dead, err = renameLedger(tx, deadLetters, prefix, []string{"name", "error", "failed_at"}); err != nil {
			return err
		}
		report.Claims, report.Dead = total(claims), total(dead)
		// The write that finds none is not written: no record, no trail row
		// saying something happened. Every later run of this step — the job's, and
		// every boot after the first — takes this branch.
		if report.Claims == 0 && report.Dead == 0 {
			return nil
		}
		// One record per tenant whose ledger moved, in the transaction that moved
		// it, so the rows and their account commit together or not at all. The
		// actor is PublishFor's to read off the context: a boot carries none, which
		// is what "nobody did this" means in the envelope, and the installation's
		// verb carries the operator's.
		tenants := make([]uuid.UUID, 0, len(claims)+len(dead))
		for id := range claims {
			tenants = append(tenants, id)
		}
		for id := range dead {
			if _, twice := claims[id]; !twice {
				tenants = append(tenants, id)
			}
		}
		report.Tenants = int64(len(tenants))
		for _, id := range tenants {
			rec := LedgerMovedRecord{App: app.String(), Durables: durables,
				Claims: claims[id], Dead: dead[id], RequestedBy: requestedBy}
			if err := PublishFor(ctx, tx, id, EventLedgerMoved, rec); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return MoveReport{}, err
	}
	return report, nil
}

// notASlug is the shared half of the refusal of a name that is set but is not a
// slug, worded once so Consume, AddressMismatch and the move cannot drift apart on
// what the operator has to fix.
const notASlug = "it is not an app name: no durable can be formed from it, so no subscription of its own could show a delivery belongs to it"

// lockLedger takes the move's lock over one ledger, refusing rather than waiting.
// The sentence names what is in the way and that nothing moved, because the retry
// is correctable and the operator has to be able to tell that from a failure that
// lost rows.
func lockLedger(ctx context.Context, tx db.Tx[db.System], table string) error {
	if err := tx.DB().Exec("LOCK TABLE " + table + " IN SHARE ROW EXCLUSIVE MODE NOWAIT").Error; err != nil {
		return fmt.Errorf("events: move the delivery ledger: a delivery is mid-claim on %s; no ledger row moved, run it again: %w", table, err)
	}
	return nil
}

// renameLedger moves one ledger's unscoped rows under prefix and answers how many
// of them each tenant had.
//
// Two statements, in this order, because the second has to be provably safe rather
// than probably: the copy runs first, and the delete then removes exactly the rows
// the copy read. Both are pinned to the same predicate, and the table lock above is
// what makes "read" and "now" the same set — no INSERT or DELETE of a claim can be
// open between the two statements.
//
// The copy is INSERT ... SELECT over a MATERIALIZED CTE rather than a bare
// INSERT ... SELECT against the same table: the CTE is evaluated once from the
// statement's own snapshot, so the rows this statement inserts cannot re-enter the
// set it is copying. ON CONFLICT DO NOTHING keeps the scoped row when a twin is
// already there — the state a move leaves behind if it ever stopped between the two
// statements — and the unscoped twin is still deleted, so the durable ends up named
// one way. The timestamp is copied, not re-stamped: handled_at is what the purge
// ages on (relay.go's purge), and a row that got a new handled_at would outlive its
// own outbox row for the accident of being moved.
//
// The delete returns what it removed, grouped by tenant, so the record names the
// tenants whose ledger moved rather than the tenant that ran the job — which is the
// difference between an audit trail and a log line.
func renameLedger(tx db.Tx[db.System], table, prefix string, columns []string) (map[uuid.UUID]int64, error) {
	carry := strings.Join(columns, ", ")
	copyStmt := `WITH src AS MATERIALIZED (
	   SELECT event_id, durable, tenant_id, ` + carry + ` FROM ` + table + ` WHERE ` + unscoped + `
  ) INSERT INTO ` + table + ` (event_id, durable, tenant_id, ` + carry + `)
      SELECT event_id, ?||durable, tenant_id, ` + carry + ` FROM src
      ON CONFLICT (event_id, durable) DO NOTHING`
	if err := tx.DB().Exec(copyStmt, prefix).Error; err != nil {
		return nil, fmt.Errorf("events: move the delivery ledger: copy %s: %w", table, err)
	}
	movedStmt := `WITH moved AS (
	   DELETE FROM ` + table + ` WHERE ` + unscoped + ` RETURNING tenant_id
  ) SELECT tenant_id, count(*)::bigint AS moved FROM moved GROUP BY tenant_id`
	type row struct {
		TenantID uuid.UUID
		Moved    int64
	}
	var rows []row
	if err := tx.DB().Raw(movedStmt).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("events: move the delivery ledger: move %s: %w", table, err)
	}
	out := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		out[r.TenantID] = r.Moved
	}
	return out, nil
}

func total(counts map[uuid.UUID]int64) int64 {
	var n int64
	for _, c := range counts {
		n += c
	}
	return n
}
