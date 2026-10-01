# Change module

`modules/change` is a proposal over somebody else's row: this diff, against that
exact revision, put forward by one account and applied only after a different one
says yes. It owns four things and nothing else — the digest of the bytes a verdict
was made about, the state machine that makes a decision one-way, the actor rule
that an author cannot be a decider, and the apply that writes once at the revision
it was reviewed at or not at all.

Which writes need it is not one of them. There is no `rest.Spec` here (a Spec is
five routes on a collection, three of which write whatever the body says; a
proposal moves through four named commands with an actor rule each), no HTML, no
nav entry, no job, and no flag read: a capability that decided which of its host's
operations were sensitive would be a capability with a product in it.

## Composition

    change.Module(change.Deps{Subjects: []contracts.SubjectBinding{{
        Module: "site", Entity: "settings",
        New:     func() json.RawMessage { … },        // the subject's current JSON
        Resolve: func() (contracts.Subject, error) { … }, // built per command
    }}})

**Reused** — `kit/crud.GetForUpdate` for the row lock and `kit/db.Tx[db.Tenant]`
for the caller's transaction; `kit/events.Publish` for the one event per
transition, in the same transaction as the state change; `kit/rest`'s `Fault`,
`Page` and `Item` for the answers, and `kit/httpx.Register` for the six operations,
which is the shape `modules/audit/internal/handler.go` already uses for a resource
that is not a Spec; `kit/problem` and `kit/fault`'s sentinels for the refusals;
`modules/audit` by subscription — the module emits events and is audited by having
emitted them, with no call into the trail; `kit/tenancy.ActorFrom` for the actor
and `tenancy.Policy` for the optional refinement of *which* proposals a decider
may decide, which is `modules/task`'s `NewServiceWithPolicy` shape;
`modules/audit/migrations/000010_audit.up.sql` as the RLS and rule-table model for
the new table.

**Added** — `contracts.Subject`, `SubjectBinding` and `Gate`, because a generic
proposal object has to write a row it does not own and there was no port for "the
module that owns it locks and saves it"; `contracts.Diff` with `Canonical` and
`Digest` in the contract package, because a verdict is about bytes and two
implementations that disagree about which bytes were reviewed is the failure this
object exists to prevent (RFC 7386, decoded with `UseNumber` so `1` and `1.0` stay
two numbers — `evanphx/json-patch` was named and refused in the specification: it
applies RFC 6902, whose `move` and array semantics a reviewer cannot eyeball).
Nothing existing could carry either: the reuse inventory ran `grep -rn "Proposer"`
across `kit modules apps ui` and found nothing, and the one working implementation
of this pattern is a client's fee review, which is the reason for the object and
not a unit the kernel could import.

**Made reusable** — the port list: any module with a row worth protecting can be
proposed-and-applied by writing `Lock` and `Save` over its own table, and any host
can decide which operations need it without this module reading a flag or a
product constant. Also the four-eyes refusal shape — `ErrSelfReview` classified
*immutable*, so the answer names the second account rather than an edit to the
request — and the derived-not-stored staleness rule, which is the lesson the
client's bespoke review already learned.

## States

`proposed → approved → applied`, `proposed → declined`, `proposed|approved →
withdrawn`. Every transition publishes exactly one event
(`change.proposal_proposed`, `_reviewed`, `_applied`, `_withdrawn`), and a command
against a terminal state returns the row unchanged and publishes nothing — the
kernel's idempotency law. Staleness is never stored: `Apply` re-reads the subject's
revision under lock and refuses `ErrStaleBase` if it has moved, leaving the
proposal `approved` and the subject untouched.

## Authorization

`change:read`, `change:propose`, `change:decide`, declared in `module.go` so the
boot gate checks every route against them. The actor comes from
`tenancy.ActorFrom(ctx` and nowhere else: `NewProposal` has no `Proposer` field and
`Review` has no `Reviewer` field, and a body that sends one is refused. Reviewer ≠
proposer and applier ≠ proposer are checked inside each command against the row's
own `proposer`, because a grant two people both hold is exactly the case the rule
is about.

## The table

`modules/change/migrations/000034_change.up.sql`, owner `change`, with no `Adopts`
(nothing ever applied these bytes under another owner) and no `RulesFrom` (every
file is guarded). Subject is `(subject_module, subject_entity, subject_id)` with no
foreign key — the reason `modules/task` already gives for `Source` — and the
partial unique index over `(tenant, subject, diff_digest) WHERE state IN
('proposed','approved')` is what makes a second submit of the same diff the same
proposal rather than a race. ENABLE / FORCE ROW LEVEL SECURITY plus
`platformkit_tenant_match(tenant_id)` on both sides; the version is 34 and not 30
because apps/platformkit's legacy-layout fixture flattens every owner's files under
one owner and refuses a repeated version — see `modules/audit/migrations.go`.

## Limits

Not composed: no application links `modules/change` yet, so no request reaches the
six operations, the boot gate never sees the manifest, and
`apps/platformkit/testdata/openapi.json` does not contain the routes. Composing it
means the subject bindings above, the flag adapter, the package-budget line, and
the document regenerated by whatever owns it.

Also not here, each one named rather than hidden: the `changetest` fake and the
C1–C14 conformance suite the specification lists; a test of two concurrent applies
under `-race` (`./modules/change/...` is not in `Makefile`'s `RACE_PACKAGES`);
`modules/site`'s `revision` column, `Gate` port and `Subject` implementation, so
the first consumer is still unwritten; and a review queue, which is a screen and
therefore belongs to whoever composes one.
