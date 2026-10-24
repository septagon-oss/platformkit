package events

// Ledger movement renames this app's tenants' handled and terminal claims onto
// scoped durables in one system transaction. Tenant and durable locks protect
// placement and active claims; each affected tenant receives an outbox record.
// Delivery independently consults both durable spellings. See README.md for
// rollout ordering, ownership and concurrency limits.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

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
// durable, refusing rather than waiting; refuse again if any claim still owes an
// effect; copy each ledger's scoped twin; delete the unscoped rows, whose twin the
// copy just provably made; and publish one record per tenant. A refusal writes
// nothing and emits nothing.
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
		// has ever committed (migrations/000058). See holdTenants for why the
		// durable list below cannot be the whole question.
		if err := holdTenants(tx, app); err != nil {
			return err
		}
		from, err := ledgerDurables(tx, app)
		if err != nil {
			return err
		}
		report.Subscriptions = len(from)
		// A claim that still owes an effect does not change hands. The rename writes the
		// scoped twin and deletes what it copied, and the copy carries the columns a
		// finished delivery leaves behind; an outstanding effect would be left naming a
		// durable this move just deleted, so the debt would be paid by moving it and the
		// scoped consumer would read a claim that owes nothing — the acknowledgement this
		// ledger exists to make impossible. Refusing is the whole remedy and it drains:
		// the effect that is owed reaches this row by the name it was written under, and
		// the run after it comes back finds the count at zero and moves it.
		if owed, err := claimsOwing(tx, app); err != nil {
			return err
		} else if owed > 0 {
			return fmt.Errorf("events: move the delivery ledger: %d of app %s's claims still owe an effect that has not run, so no ledger row moved; %w", owed, app, ErrLedgerMoveContended)
		}
		durables := make([]string, len(from))
		for i, d := range from {
			durables[i] = prefix + d
		}
		var (
			claims map[uuid.UUID]int64
			dead   map[uuid.UUID]int64
		)
		if claims, err = renameLedger(tx, handled, prefix, []string{"handled_at"}, from, app); err != nil {
			return err
		}
		if dead, err = renameLedger(tx, deadLetters, prefix, []string{"name", "error", "failed_at"}, from, app); err != nil {
			return err
		}
		report.Claims, report.Dead = total(claims), total(dead)
		// Recheck membership after both renames so an unscoped row appearing
		// outside the entry lock set refuses the entire transaction.
		if remains, err := unscopedRemains(tx, app); err != nil {
			return err
		} else if remains {
			return fmt.Errorf("events: move the delivery ledger: app %s's ledger still names an unscoped row after the rename, so a tenant took the app while the move read it; %w", app, ErrLedgerMoveContended)
		}
		// The write that finds none is not written: no record, no trail row
		// saying something happened. Every later run of this step — the job's, and
		// every boot after the first — takes this branch.
		if report.Claims == 0 && report.Dead == 0 {
			return nil
		}
		report.Tenants, err = publishLedgerMove(ctx, tx, app, requestedBy, durables, claims, dead)
		return err
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
// hash to be wrong in. The text is hashed by migrations/000058's trigger too: the
// claim that takes the matching share lock and the move that asks for this one are
// two halves of one key, and only this function states it in Go.
func durableLock(durable string) string { return "events ledger " + durable }

// tenantLockKey is the expression the claim trigger hashes: migrations/000058's text,
// spelled here so the move that asks this key for an app and the claim that takes its
// share are two halves of one key and cannot drift — a move that asks the wrong key
// excludes nothing and reports a safe refusal it never made. It is the first key the
// claim takes and the first the move asks for: the move holds a tenant's key before it
// asks any durable's, and the trigger takes the tenant's before it can queue behind a
// durable's, because a lock taken second excludes nothing the first is waiting behind,
// and a claim queued on a durable with its own tenant still unnamed is a claim this
// statement cannot see.
const tenantLockKey = `'events ledger tenant ' || id::text`

// tenantLock is that same key as Go can name it, for the half of the pair that is a
// tenant id in hand rather than a row in a table: uuid's text is the lowercase
// hyphenated form Postgres prints for `id::text`, so the two spellings hash alike.
// The argument is durableLock's, and the reason both halves of both keys sit here.
func tenantLock(tenantID uuid.UUID) string { return "events ledger tenant " + tenantID.String() }

// declareTenant holds the tenant's shared ledger lock until tx ends, before any
// placement read or claim write. It waits behind an active move; placement takes
// the same key before naming the tenant, so the following read is authoritative.
func declareTenant(gdb *gorm.DB, tenantID uuid.UUID) error {
	if err := gdb.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?, 0))`, tenantLock(tenantID)).Error; err != nil {
		return fmt.Errorf("events: declare the delivery ledger of tenant %s busy: %w", tenantID, err)
	}
	return nil
}

// unscopedDurable is Go's reading of the move's `unscoped` predicate and of
// migrations/000058's WHEN clause, so a writer of a claim and the trigger that locks it
// cannot disagree about which rows the tenant key is for: '+' is appJoin, in none of the
// three grammars an app, a module or an event name is written in.
func unscopedDurable(durable string) bool { return !strings.Contains(durable, "+") }

// placeable is the predicate that says a tenant could still become this app's by the
// end of the move: it is the app's already, or it names no app and a placement is the
// act that names one. See holdTenants for why the move locks both halves. The set is
// closed from both sides now: the move holds the key of every tenant the predicate names,
// and the walk that turns an empty app into a slug declares that key before it writes
// (migrations/000061), so membership cannot move underneath a running move at all.
const placeable = "app = ? OR app = ''"

// holdTenants exclusively try-locks this app's tenants and still-unplaced tenants
// before discovering durable rows. Active claims or placements refuse the move;
// locks on unplaced tenants cover membership that could join during the move.
// Named tenants of other apps remain independent. See README.md for rollout costs.
func holdTenants(tx db.Tx[db.System], app appname.Name) error {
	var contended int64
	if err := tx.DB().Raw(`SELECT count(*) FROM (
		   SELECT pg_try_advisory_xact_lock(hashtextextended(`+tenantLockKey+`, 0)) AS held
		     FROM tenants WHERE `+placeable+`) AS claims WHERE NOT held`, string(app)).Scan(&contended).Error; err != nil {
		return fmt.Errorf("events: move the delivery ledger: the locks on app %s's tenants: %w", app, err)
	}
	if contended > 0 {
		return fmt.Errorf("events: move the delivery ledger: a delivery is mid-claim in %d of app %s's tenants or of tenants no app is placed under yet, any of which this move could rename; %w",
			contended, app, ErrLedgerMoveContended)
	}
	return nil
}

// unscopedRemains asks whether any unscoped row sits in one of app's tenants once the
// rename has run. It is the move's own `unscoped` and `ownTenant` predicates, asked of the
// state it is about to commit rather than of the state it read, and it has one honest
// answer: nothing remains, because the durables it renames are the durables the read above
// found under exactly this predicate, and no claim can be open in a tenant whose key the
// move holds. Anything else says the *tenants* changed underneath the move — the placement
// named a row the entry lock set could only call placeable — and the move that cannot
// prove its set proves the opposite, so it refuses and writes nothing. A refusal here is
// rarer than the one the entry makes and it is the same refusal: a placement is a walk that
// drains, and the retry reads a membership that holds still.
func unscopedRemains(tx db.Tx[db.System], app appname.Name) (bool, error) {
	var remains bool
	// The slug binds twice: the reading names one ledger's tenants and then the other's.
	if err := tx.DB().Raw(`SELECT EXISTS (
		   SELECT 1 FROM `+handled+` WHERE `+unscoped+` AND `+ownTenant+`)
		OR EXISTS (
		   SELECT 1 FROM `+deadLetters+` WHERE `+unscoped+` AND `+ownTenant+`)`, string(app), string(app)).Scan(&remains).Error; err != nil {
		return false, fmt.Errorf("events: move the delivery ledger: check what is left unscoped for app %s: %w", app, err)
	}
	return remains, nil
}

// claimsOwing counts app's unscoped claims still counting an effect their delivery
// deferred past its own commit — the rows the rename below is about to make a new
// primary key of, and the reason that rename refuses while any of them stands.
//
// It is asked after the durable locks are held and of the same set of rows the rename
// names (unscoped, in a tenant this app holds), so the answer is the one the rename
// would act on. What it cannot be overtaken by is worth naming: a claim that grows its
// count has inserted its row in the same transaction, and that INSERT takes this move's
// durable key and its tenant key (migrations/000058) for as long as the delivery runs;
// an effect coming back only ever lowers a count, and lowering one after this reading
// costs a move a run it repeats, never a row it lost. A ledger that does not count owed
// effects owes none: see ledgerCountsEffects.
func claimsOwing(tx db.Tx[db.System], app appname.Name) (int64, error) {
	counting, err := ledgerCountsEffects(tx)
	if err != nil {
		return 0, err
	}
	if !counting {
		return 0, nil
	}
	var owing int64
	if err := tx.DB().Raw(`SELECT count(*) FROM `+handled+` WHERE `+unscoped+` AND `+ownTenant+` AND effects_owed > 0`,
		string(app)).Scan(&owing).Error; err != nil {
		return 0, fmt.Errorf("events: move the delivery ledger: ask what app %s's claims still owe: %w", app, err)
	}
	return owing, nil
}

// ledgerCountsEffects reports whether this installation's delivery ledger carries the
// owed-effect count column.
//
// The column is this release's own (migrations/000075), and the runner applies nothing of
// an owner behind a phase=data file until that file's drain finishes, so an installation
// upgrading through a release that ships both runs this code over the ledger as it was
// before the column — and this move is the step that meets that state, because kit/app
// holds every subscription and every job, that drain among them, until the move returns.
// Absent, nothing has ever raised a count, so the answer is zero by the shape of the
// write and the rename goes ahead; present, the count is asked and a claim that owes does
// not change hands. kit/events/README.md states the window beside the move itself.
func ledgerCountsEffects(tx db.Tx[db.System]) (bool, error) {
	var columns int64
	// to_regclass resolves the bare name through this session's search_path, which is the
	// relation the count above would have read; a catalog row for a different table named
	// the same in some other schema is not this answer.
	if err := tx.DB().Raw(`SELECT count(*) FROM pg_attribute
			WHERE attrelid = to_regclass(?) AND attname = 'effects_owed' AND attnum > 0 AND NOT attisdropped`,
		handled).Scan(&columns).Error; err != nil {
		return false, fmt.Errorf("events: move the delivery ledger: ask whether the ledger counts owed effects: %w", err)
	}
	return columns > 0, nil
}

// holdDurable takes the move's lock over one durable's rows, refusing rather than
// queueing. The sentence names what is in the way and that nothing moved, because
// the retry is correctable and the operator has to be able to tell this from a
// failure that lost rows.
// ErrLedgerMoveContended is the move's answer to a delivery in flight: one claim is
// open under a durable this move is about to rename, or one still counts an effect its
// delivery has not carried out, so the whole move refuses and writes nothing. It is a
// sentinel because the operator's door has to be able to tell "ask again" from
// "something is broken" without matching a sentence — the refusal reads as a conflict
// there, and as the same refusal in a log here.
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

// renameLedger copies and deletes the same materialized set of unscoped claims,
// returning removed counts by tenant. Existing scoped twins survive conflicts,
// and original timestamps are retained. Using exact source keys for deletion
// prevents a newly visible claim from being deleted without a copied twin.
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
	// One statement, three CTEs: read the set once, copy it under the prefix, delete
	// exactly what was read. The delete's IN-list is the key the copy was built from,
	// so the two cannot disagree about a row however the tables around them move.
	moveStmt := `WITH src AS MATERIALIZED (
	   SELECT event_id, durable, tenant_id, ` + carry + ` FROM ` + table + ` WHERE ` + where + `
  ), copied AS (
	   INSERT INTO ` + table + ` (event_id, durable, tenant_id, ` + carry + `)
	     SELECT event_id, ?||durable, tenant_id, ` + carry + ` FROM src
	     ON CONFLICT (event_id, durable) DO NOTHING
  ), moved AS (
	   DELETE FROM ` + table + ` AS t
	    WHERE (t.event_id, t.durable) IN (SELECT event_id, durable FROM src)
	    RETURNING t.tenant_id
  ) SELECT tenant_id, count(*)::bigint AS moved FROM moved GROUP BY tenant_id`
	type row struct {
		TenantID uuid.UUID
		Moved    int64
	}
	var rows []row
	if err := tx.DB().Raw(moveStmt, append(args, prefix)...).Scan(&rows).Error; err != nil {
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

func ledgerDurables(tx db.Tx[db.System], app appname.Name) ([]string, error) {
	// The names this app's tenants still hold claims under, read before any lock
	// is taken: of the un-scoped rows, only the ones in a tenant this app holds.
	var from []string
	if err := tx.DB().Raw(`SELECT DISTINCT durable FROM `+handled+` WHERE `+unscoped+`
			AND `+ownTenant+`
			UNION
			SELECT DISTINCT durable FROM `+deadLetters+` WHERE `+unscoped+`
			AND `+ownTenant+` ORDER BY durable`, string(app), string(app)).Scan(&from).Error; err != nil {
		return nil, fmt.Errorf("events: move the delivery ledger for app %s: name the durables: %w", app, err)
	}
	// Both ledgers share one durable lock. Refuse active claims under that
	// legacy name without excluding unrelated durables or scoped deliveries.
	for _, d := range from {
		if err := holdDurable(tx, d); err != nil {
			return nil, err
		}
	}
	return from, nil
}

func publishLedgerMove(ctx context.Context, tx db.Tx[db.System], app appname.Name, requestedBy string, durables []string, claims, dead map[uuid.UUID]int64) (int64, error) {
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
	count := int64(len(tenants))
	for _, id := range tenants {
		rec := LedgerMovedRecord{App: app.String(), Durables: durables,
			Claims: claims[id], Dead: dead[id], RequestedBy: requestedBy}
		if err := SucceedFor(ctx, tx, db.Now()); err != nil {
			return 0, err
		}
		if err := PublishFor(ctx, tx, id, EventLedgerMoved, rec); err != nil {
			return 0, err
		}
	}
	return count, nil
}
