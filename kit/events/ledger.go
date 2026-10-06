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
// Which is why the move cannot be what a delivery rests on, and does not have to be.
// A placement is another replica's boot, and the write that names a tenant is also the
// write that hands that tenant's pending rows to this app's relay — so a scoped
// consumer that opened before the placement can be shown an event whose mark this
// rename has not seen, and a rename that lands after the delivery cannot undo what the
// delivery committed. The delivery therefore reads the tenant's own history under both
// spellings of one subscription's durable (events.go's claim) and refuses on its own;
// this move is what makes that history findable by the name the app now answers to —
// the record a reviewer reads, the row events.Replay clears, the terminal mark claim
// refuses beside. Renaming is tidiness and audit. Refusing the second handling is not
// scheduled, and so cannot be late.
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
		// The membership re-check, last, in the transaction that did the renaming. The
		// locks above describe the membership as of their own reading; this statement asks
		// the same question of the state the rename ends in, and a difference is an answer
		// the move cannot keep. It is what catches the tenant this move holds no key for
		// because it did not exist when the entry read ran and was named anyway: the row it
		// writes is unscoped, its claims arrive under a name its consumer is about to
		// subscribe under, and the second handling has no other witness. The walk that names
		// an app now declares the tenant's own key before it writes (migrations/000047), so a
		// placement can no longer commit between this transaction's reads — refused over by
		// the entry lock if it came first, serialized behind it if it came after, and owing
		// the move its own boot runs next. Refusing is the whole remedy: nothing moved,
		// nothing emitted, and the retry after this transaction ends reads a membership that
		// cannot move again while it holds the keys.
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

// tenantLock is that same key as Go can name it, for the half of the pair that is a
// tenant id in hand rather than a row in a table: uuid's text is the lowercase
// hyphenated form Postgres prints for `id::text`, so the two spellings hash alike.
// The argument is durableLock's, and the reason both halves of both keys sit here.
func tenantLock(tenantID uuid.UUID) string { return "events ledger tenant " + tenantID.String() }

// declareTenant takes the shared half of one tenant's ledger key for the rest of tx,
// before that transaction reads or writes anything about a claim.
//
// The row trigger takes this key, and for a claim already inserted that is enough: the
// mark and its key belong to one transaction. It is not enough for a claim that has not
// reached its INSERT yet, and the wait is what says so — a SHARE table lock, the one
// CREATE INDEX takes, or a queue behind any writer, holds the statement *before* the row
// trigger runs, so the transaction declares nothing while it waits. A placement commits
// under it, this tenant's move then finds no key held and no committed row, answers its
// zero report as a success, and the claim that finally lands is a mark under a durable
// the tenant's own app will never subscribe under — the second handling this key exists
// to prevent, arriving by the door the key's own placement left open. Declaring the
// tenant above the read the
// placement can overtake is what makes that read an answer the move has to respect: from
// this statement to the end of the transaction, the move of this tenant refuses. The
// placement declares the same key before its own write, for the same reason and with the
// same lifetime (migrations/000047), and that is the half that made this one enough: the
// two writers that can change what a move is renaming are the claim and the naming, and
// both are now visible from before the statement that decides to write.
//
// It waits rather than refuses, which is the direction a delivery wants: the only holder
// it can wait behind is a move's own transaction, which asks the key exclusively, renames
// or refuses, and ends — and the wait costs an app-less deployment nothing it does not
// already pay at the claim's trigger for the same key, since both hold it to commit.
func declareTenant(gdb *gorm.DB, tenantID uuid.UUID) error {
	if err := gdb.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?, 0))`, tenantLock(tenantID)).Error; err != nil {
		return fmt.Errorf("events: declare the delivery ledger of tenant %s busy: %w", tenantID, err)
	}
	return nil
}

// unscopedDurable is Go's reading of the move's `unscoped` predicate and of
// migrations/000044's WHEN clause, so a writer of a claim and the trigger that locks it
// cannot disagree about which rows the tenant key is for: '+' is appJoin, in none of the
// three grammars an app, a module or an event name is written in.
func unscopedDurable(durable string) bool { return !strings.Contains(durable, "+") }

// placeable is the predicate that says a tenant could still become this app's by the
// end of the move: it is the app's already, or it names no app and a placement is the
// act that names one. See holdTenants for why the move locks both halves. The set is
// closed from both sides now: the move holds the key of every tenant the predicate names,
// and the walk that turns an empty app into a slug declares that key before it writes
// (migrations/000047), so membership cannot move underneath a running move at all.
const placeable = "app = ? OR app = ''"

// holdTenants takes the move's lock over every tenant that could still be this app's
// by the end of the move, refusing rather than queueing, before a single ledger row is
// read.
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
// order — and the claim's transaction takes that key above its first read of all
// (declareTenant, called by holdsUnscoped and deadLetter), so a claim is visible to its
// app's move from before the statement that decides whether to write one, whatever the
// INSERT itself then waits behind. A delivery of another app holds its own tenant's lock
// and refuses nothing here, which is what the per-durable lock already buys and this keeps.
//
// The set is `placeable`, app's own tenants *and* the ones still naming nobody, because a
// tenant is placed by a walk this transaction has no part in. Reading only this app's rows
// describes the membership at the instant of the read and nothing after it: an unplaced
// tenant's open claim declares a key this move never asks for, so the move takes no key for
// it, reads no row for it (`ownTenant` below answers false while the row is still empty),
// answers its zero report as a success, and the placement that commits while it is open puts
// the tenant — and the claim that lands a statement later — inside this app, under a durable
// its consumer will never subscribe under. The hazard is a claim in a tenant that *becomes*
// this app's during the move, so the lock set has to be the set that can become this app's,
// and the placement's own rule says that set: an app, once written onto a row, never moves
// and never changes (migrations/000043, and 000045's write checks `app = ”` at the write
// itself), so the only tenants that can join this app are the empty ones. A claim in one of
// them is refused over with the app's own, which is the same refusal, written down where the
// membership is what is in the way.
//
// The set was half of it, and the review that found the other half is
// kit/events/placement_before_ledger_commit_test.go. A tenant holding an already-committed
// unscoped claim declares nothing until its *next* claim, so a key held over the claim
// excluded no walk that never claims one: the placement could write the row, commit, and
// be gone before the move's final read, which answered false because the tenant was still
// empty when it asked, and the move committed its success over a ledger it had never read.
// The other half is the placement declaring this key before it names the row
// (migrations/000047's trigger, the same expression as tenantLockKey below): the walk
// either holds the key when the move asks, and the move refuses over it, or waits and
// lands after the move committed — the order that owes a move, which is the order a boot
// that places before it opens its scoped consumers actually pays.
//
// The widening costs an app's move a refusal whenever an app-less delivery is open
// anywhere, and that is the direction that keeps rows: the traffic that refuses it is the
// traffic whose claims this move would otherwise strand, and it is the traffic that stops by
// the end of a rollout that names its app. It is not a wider lock over the other app, which
// is the harm a table lock would dress up as safety: academy's tenant has an app and is not
// in the set, and an app-less delivery holds a key only for the move that is running.
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
// Two halves in one statement, because the second has to be provably safe rather than
// probably: the delete removes the rows the copy read, keyed by the keys the copy read,
// and not by a predicate asked a second time. Both halves used to spell the same
// predicate — the unscoped name set the locks above were taken for, in the tenants this
// app holds — and that was one assumption deep: a row's own durable never changes, but
// `ownTenant` answers a question about another table, and a placement can commit between
// the copy and the delete. A tenant that took this app in that gap has its unscoped rows
// inside the delete's reading and outside the copy's, and the pair then removes a claim it
// never copied — the mark that stops a handler running twice, taken away by the move whose
// whole reason is to stop handlers running twice. So the delete asks for `src`'s
// (event_id, durable) pairs, which are the primary key: one reading, one set, and no
// statement in between that could change what the other means.
//
// The copy is INSERT ... SELECT over a MATERIALIZED CTE rather than a bare
// INSERT ... SELECT against the same table: the CTE is evaluated once from the
// statement's own snapshot, so the rows this statement inserts cannot re-enter the
// set it is copying, and the delete, which reads the same CTE, cannot see a set wider
// than the one the insert carried. ON CONFLICT DO NOTHING keeps the scoped row when a twin
// is already there — the state a move leaves behind if it ever stopped between the two
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
