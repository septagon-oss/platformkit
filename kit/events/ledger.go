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
// the others, which is an app skipping work it never did.
//
// Whose rows, then. A claim names no app, but it names its tenant, and a tenant
// belongs to exactly one app (tenants.app, migrations/000043): the claim is work an
// app's own consumer did in its own tenant. So the move renames the unscoped claims
// of the tenants this app holds, and no others — which is T-0228's plan for this
// move, read from kit/appname's §Limits: "renaming durable for the tenants whose
// tenants.app is this app's slug". A predicate on the durable alone would let the
// first app to move rename the whole database's unscoped history, including the
// claims the other app made in its own tenants; that other app's consumer would then
// look for (event_id, "academy+mod-ev"), find nothing — its claim is sitting under
// collect's durable — and run a handler for work its own tenant already committed.
// An app that moves would thus cause in the other app exactly the double-handling it
// moved to close in itself.
//
// A tenant whose tenants.app is empty belongs to no named app, and this move leaves
// its claims where the app-less consumer looks for them: an app-less deployment on
// this database is a real deployment (kit/appname §Limits), and taking its ledger
// would be the write that takes the last claim away. The placement onto phase=data
// (SPECIFY item 3, unbuilt) is what turns an empty app into a slug; until it has
// run, those tenants are nobody's to move.
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
	"errors"
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

// ownTenant is the predicate that says the row's tenant belongs to the app whose
// durable is being formed. One bound parameter: the slug. A claim records work an
// app's own consumer did in a tenant it holds, so this is the boundary that keeps
// one app's move out of another app's ledger — see the header.
const ownTenant = "tenant_id IN (SELECT id FROM tenants WHERE app = ?)"

func durableSet(n int) string {
	return "durable IN (" + strings.TrimSuffix(strings.Repeat("?,", n), ",") + ")"
}

// MoveLedger renames this deployment's handled and dead-letter rows onto the
// durables of app, and records the act in every tenant whose ledger moved.
//
// One transaction, in this order, each step a statement a reviewer can read: name
// the durables of this app's tenants that will move; take one lock per named
// durable, refusing rather than waiting; copy each ledger's scoped twin; delete the
// unscoped rows, whose twin the copy just provably made; and publish one record per
// tenant. A refusal writes nothing and emits nothing.
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
		// The tenants first, before any name is read: a claim declares its tenant
		// busy for its whole transaction, whether or not any row under that durable
		// has ever committed (migrations/000044). See holdTenants for why the
		// durable list below cannot be the whole question.
		if err := holdTenants(tx, app); err != nil {
			return err
		}
		// The names this app's tenants still hold claims under, read before any lock
		// is taken: of the un-scoped rows, only the ones in a tenant this app holds.
		var from []string
		if err := tx.DB().Raw(`SELECT DISTINCT durable FROM `+handled+` WHERE `+unscoped+`
				AND `+ownTenant+`
				UNION
				SELECT DISTINCT durable FROM `+deadLetters+` WHERE `+unscoped+`
				AND `+ownTenant+` ORDER BY durable`, string(app), string(app)).Scan(&from).Error; err != nil {
			return fmt.Errorf("events: move the delivery ledger for app %s: name the durables: %w", app, err)
		}
		// One lock per named durable, refusing rather than waiting. A delivery takes
		// the matching shared lock in claim before it writes its mark, so an
		// exclusive try-lock that answers false says a claim under this durable is
		// open right now. Nothing else needs excluding: a delivery under some *other*
		// durable writes no row this move reads, and a delivery under the *scoped*
		// twin takes no lock at all, because the move never renames a scoped row. A
		// table lock here is simpler to write and wrong to run — with two apps on one
		// database it lets the other app's ordinary traffic hold this app's boot in a
		// refusal that never drains, which is the window this move exists to close.
		// Both ledgers share the one key, because one durable's rows move together.
		for _, d := range from {
			if err := holdDurable(tx, d); err != nil {
				return err
			}
		}
		report.Subscriptions = len(from)
		durables := make([]string, len(from))
		for i, d := range from {
			durables[i] = prefix + d
		}
		var (
			claims map[uuid.UUID]int64
			dead   map[uuid.UUID]int64
			err    error
		)
		if claims, err = renameLedger(tx, handled, prefix, []string{"handled_at"}, from, app); err != nil {
			return err
		}
		if dead, err = renameLedger(tx, deadLetters, prefix, []string{"name", "error", "failed_at"}, from, app); err != nil {
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

// durableLock is the key one durable's rows are written and renamed under. The
// text itself is the key, hashed into the 64-bit advisory space with the same
// call modules/auth uses for its own keys: two durables that collide answer with
// a refusal that writes nothing and is retried, which is the cheap direction for a
// hash to be wrong in. The text is hashed by migrations/000044's trigger too: the
// claim that takes the matching share lock and the move that asks for this one are
// two halves of one key, and only this function states it in Go.
func durableLock(durable string) string { return "events ledger " + durable }

// tenantLockKey is the expression the claim trigger hashes: migrations/000044's text,
// spelled here so the move that asks this key for an app and the claim that takes its
// share are two halves of one key and cannot drift — a move that asks the wrong key
// excludes nothing and reports a safe refusal it never made. It is the first key the
// claim takes and the first the move asks for: the move holds a tenant's key before it
// asks any durable's, and the trigger takes the tenant's before it can queue behind a
// durable's, because a lock taken second excludes nothing the first is waiting behind,
// and a claim queued on a durable with its own tenant still unnamed is a claim this
// statement cannot see.
const tenantLockKey = `'events ledger tenant ' || id::text`

// holdTenants takes the move's lock over every tenant of app, refusing rather than
// queueing, before a single ledger row is read.
//
// The durable locks below are taken over the names this app's tenants hold
// *committed* rows under, and a committed row is exactly what the first delivery of
// a subscription does not have yet. An empty ledger therefore names nothing to lock,
// and a move that discovers nothing returns its zero report as a success — while a
// claim that is open right now, under a durable no row has ever named, commits a
// mark under the unscoped name a moment later. Nothing then finds it: the scoped
// consumer looks for (event_id, "collect+mod-ev"), the row says "mod-ev", and the
// re-publish that a consumer remake or a replay makes runs the handler a second
// time. That is the first invisible claim, and the durable locks cannot see it
// because the durable is the thing being discovered.
//
// The tenant is the boundary that can. A claim always names the tenant it was
// taken in, the placement makes a tenant belong to exactly one app (tenants.app,
// migrations/000043), and the hazard is a claim in one of *this* app's tenants
// outliving the rename of its ledger — so a claim takes the shared advisory lock of
// its own tenant *first*, before the lock of its durable and so before it can queue
// behind another move's, and the move asks for those tenants exclusively in the same
// order. A delivery of another app holds its own tenant's lock and refuses
// nothing here, which is what the per-durable lock already buys and this keeps.
func holdTenants(tx db.Tx[db.System], app appname.Name) error {
	var contended int64
	if err := tx.DB().Raw(`SELECT count(*) FROM (
		   SELECT pg_try_advisory_xact_lock(hashtextextended(`+tenantLockKey+`, 0)) AS held
		     FROM tenants WHERE app = ?) AS claims WHERE NOT held`, string(app)).Scan(&contended).Error; err != nil {
		return fmt.Errorf("events: move the delivery ledger: the locks on app %s's tenants: %w", app, err)
	}
	if contended > 0 {
		return fmt.Errorf("events: move the delivery ledger: a delivery is mid-claim in %d of app %s's tenants; %w",
			contended, app, ErrLedgerMoveContended)
	}
	return nil
}

// holdDurable takes the move's lock over one durable's rows, refusing rather than
// queueing. The sentence names what is in the way and that nothing moved, because
// the retry is correctable and the operator has to be able to tell this from a
// failure that lost rows.
// ErrLedgerMoveContended is the move's answer to a delivery in flight: one claim is
// open under a durable this move is about to rename, so the whole move refuses and
// writes nothing. It is a sentinel because the operator's door has to be able to tell
// "ask again" from "something is broken" without matching a sentence — the refusal
// reads as a conflict there, and as the same refusal in a log here.
var ErrLedgerMoveContended = errors.New("no ledger row moved, run it again")

func holdDurable(tx db.Tx[db.System], durable string) error {
	var got bool
	if err := tx.DB().Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?, 0))`, durableLock(durable)).Scan(&got).Error; err != nil {
		return fmt.Errorf("events: move the delivery ledger: the lock on %s: %w", durable, err)
	}
	if !got {
		return fmt.Errorf("events: move the delivery ledger: a delivery is mid-claim on %s; %w", durable, ErrLedgerMoveContended)
	}
	return nil
}

// renameLedger moves one ledger's unscoped rows under prefix and answers how many
// of them each tenant had.
//
// Two statements, in this order, because the second has to be provably safe rather
// than probably: the copy runs first, and the delete then removes exactly the rows
// the copy read. Both are pinned to the same predicate — the same unscoped name set
// the locks above were taken for, in the tenants this app holds — so "read" and
// "now" are the same set: no claim can be open on a durable this statement renames,
// and no row outside the named set is touched at all.
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
func renameLedger(tx db.Tx[db.System], table, prefix string, columns, from []string, app appname.Name) (map[uuid.UUID]int64, error) {
	// Nothing named, nothing to do: the caller read the names this app's tenants
	// hold, and an empty reading is the second run of an idempotent move.
	if len(from) == 0 {
		return map[uuid.UUID]int64{}, nil
	}
	carry := strings.Join(columns, ", ")
	// The durables are spelled as an IN-list of one placeholder each rather than an
	// array, because they are the names the locks were taken for and the statement
	// must be able to say that as plainly as the lock does: rename these, and no
	// other row. A claim that arrives under a name outside the set is left exactly
	// where it is, for the run that will name it.
	where := unscoped + ` AND ` + ownTenant + ` AND durable IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(from)), ",") + `)`
	args := append([]any{string(app)}, anyStrings(from)...)
	copyStmt := `WITH src AS MATERIALIZED (
	   SELECT event_id, durable, tenant_id, ` + carry + ` FROM ` + table + ` WHERE ` + where + `
  ) INSERT INTO ` + table + ` (event_id, durable, tenant_id, ` + carry + `)
      SELECT event_id, ?||durable, tenant_id, ` + carry + ` FROM src
      ON CONFLICT (event_id, durable) DO NOTHING`
	if err := tx.DB().Exec(copyStmt, append(args, prefix)...).Error; err != nil {
		return nil, fmt.Errorf("events: move the delivery ledger: copy %s: %w", table, err)
	}
	movedStmt := `WITH moved AS (
	   DELETE FROM ` + table + ` WHERE ` + where + ` RETURNING tenant_id
  ) SELECT tenant_id, count(*)::bigint AS moved FROM moved GROUP BY tenant_id`
	type row struct {
		TenantID uuid.UUID
		Moved    int64
	}
	var rows []row
	if err := tx.DB().Raw(movedStmt, args...).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("events: move the delivery ledger: move %s: %w", table, err)
	}
	out := make(map[uuid.UUID]int64, len(rows))
	for _, r := range rows {
		out[r.TenantID] = r.Moved
	}
	return out, nil
}

// anyStrings copies a []string into the argument slice Exec wants, so one ledger's
// predicate can be asked of the durables the locks were taken for.
func anyStrings(in []string) []any {
	out := make([]any, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}

func total(counts map[uuid.UUID]int64) int64 {
	var n int64
	for _, c := range counts {
		n += c
	}
	return n
}
