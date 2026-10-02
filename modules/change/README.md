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

The reference application composes it in `apps/platformkit/modules.go`, over the
one subject it applies proposals for. The literal below builds:
`readme_composition_test.go` compiles its field names, its `Resolve` signature and
the `change.Deps` and `change.Module` around them, with a subject stubbed out.

```go
// contracts is github.com/septagon-oss/platformkit/modules/change/contracts
change.Module(change.Deps{Subjects: []contracts.SubjectBinding{{
	Module: "site", Entity: "settings",
	Resolve: func(ctx context.Context, tx db.Tx[db.Tenant], subjectID uuid.UUID) (contracts.Subject, error) {
		return siteSubject{sites: sites, locked: locked}, nil
	},
}}})
```

`apps/platformkit/change.go` holds `siteSubject` (its two methods over
`sitecontracts.Service` and `sitecontracts.LockedReader`), `settingsGate` (the site
module's `WriteGate` question answered by one flag, wired into
`site.Module(site.Deps{Gate: …})`) and `configFlags` (the `kit/flags.Evaluator`
over `kit/config`'s flags block). One `SubjectBinding` per subject, written out by
one author, checked by the compiler; `change.Deps.Subjects` empty means no subject
exists and every proposal for one is `contracts.ErrUnsupportedSubject`.

**Reused** — `kit/crud.GetForUpdate` for the row lock and `kit/db.Tx[db.Tenant]`
for the caller's transaction; `kit/events.Publish` for the one event per
transition, in the same transaction as the state change; `kit/rest`'s `Fault`,
`Page` and `Item` for the answers, and `kit/httpx.Register` for the six operations,
which is the shape `modules/audit/internal/handler.go` already uses for a resource
that is not a Spec; `kit/problem` and `kit/fault`'s sentinels for the refusals;
`modules/audit` by subscription — the module emits events and is audited by having
emitted them, with no call into the trail; `kit/tenancy.ActorFrom` for the actor;
`modules/audit/migrations/000010_audit.up.sql` as the RLS and rule-table model for
the new table; `gorm`'s savepoint, which is what makes a lost insert race
recoverable at all (see `internal.Service.insert`).

Which proposals a given decider may see is *not* refined by a policy: every holder
of `change:read` in a tenant reads all of that tenant's proposals, and the one
thing that refuses a caller is the row's own `proposer`. The `tenancy.Policy`
shape `modules/task` uses for object scope is the unit a review queue would reach
for; this module does not reach for it, and does not claim to.

**Added** — `contracts.Subject` and `SubjectBinding`, because a generic proposal
object has to write a row it does not own and there was no port for "the module
that owns it locks and saves it"; `contracts.Refusal`, the answer a gated door
gives a write that has to be proposed first (its `Error` names the path and the
permission, which is the half `kit/rest` puts in the client's hands);
`contracts.Diff` with `Canonical` and `Digest` in the contract package, because a
verdict is about bytes and two implementations that disagree about which bytes were
reviewed is the failure this object exists to prevent (RFC 7386, decoded with
`UseNumber` so `1` and `1.0` stay two numbers — `evanphx/json-patch` was named and
refused in the specification: it applies RFC 6902, whose `move` and array semantics
a reviewer cannot eyeball). Nothing existing could carry either: the reuse
inventory ran `grep -rn "Proposer"` across `kit modules apps ui` and found nothing,
and the one working implementation of this pattern is a client's fee review, which
is the reason for the object and not a unit the kernel could import.

**Made reusable** — the port list: any module with a row worth protecting can be
proposed-and-applied by writing `Lock` and `Save` over its own table, and any host
can decide which operations need it by answering its own module's gate question
over `contracts.Refusal`, without this module reading a flag or a product constant.
Also the four-eyes refusal shape — `ErrSelfReview` classified *immutable*, so the
answer names the second account rather than an edit to the request — and the
derived-not-stored staleness rule, which is the lesson the client's bespoke review
already learned.

## States

`proposed → approved → applied`, `proposed → declined`, `proposed|approved →
withdrawn`. Every transition publishes exactly one event
(`change.proposal_proposed`, `_reviewed`, `_applied`, `_withdrawn`), and a command
that replays a finished transition *for the account that finished it* returns the
row unchanged and publishes nothing — the kernel's idempotency law. The same call
from an account the command refuses is a refusal with no row (Duties below).
Staleness is never stored: `Apply` re-reads the subject's revision under lock and
refuses `ErrStaleBase` if it has moved, leaving the proposal `approved` and the
subject untouched.

## Authorization

### Permissions

`change:read`, `change:propose`, `change:decide`, declared in `module.go` so the
boot gate checks every route against them and a route guarded by a key missing
here fails startup. `change:read` guards `change-proposal-list` and
`change-proposal-read`; `change:propose` guards `change-proposal-propose` and
`change-proposal-withdraw`; `change:decide` guards `change-proposal-review` and
`change-proposal-apply`. The three keys are the whole surface: nothing in this
module is reachable without one, and there is no fourth key.

### Object scope

Tenant, and within it everything: row-level security narrows the table to the
request's tenant (`FORCE ROW LEVEL SECURITY`, `platformkit_tenant_match` on both
sides) and no narrower rule applies. There is no per-row scope — no `tenancy.Policy`,
no assignee rule, no "whoever proposed it sees their own" — because the object's
point is that a *different* account reads it: a decider who could not see the
proposal could not decide it. `Query` filters by state and by subject; those are
narrowings of a list a caller is already entitled to, not authorisation.

### Duties the module enforces itself

The four-eyes rule, twice, against the row rather than against a policy server:
`Review` refuses `ErrSelfReview` when the actor is the `proposer`, and `Apply`
refuses `ErrSelfReview` when the actor is the `proposer` — so an account holding
both `change:propose` and `change:decide` can put a change forward or decide
somebody else's, but cannot complete its own. Both checks run inside the
authoritative transaction, after `crud.GetForUpdate` has the row, and a refusal
writes nothing, publishes nothing and returns no row. Each of those actor rules is
asked *before* the row's own state is answered, so retrying somebody else's withdraw
or somebody else's apply is a refusal rather than that person's row. `Withdraw`
refuses anybody but the proposer, and only while the proposal is open: a decision
that has been made is not something its subject un-says. Every mutation rechecks the
caller's `expectedRevision` against the row's own `revision` under that lock.
`Propose` deduplicates an open diff for its own author only: a second account that
submits the same bytes is answered `change:propose`'s conflict with no proposal in
it, because the row that index keeps belongs to whoever holds `change:read`.

### Public faces

None. `internal/handler.go` mounts every operation on `s.App`, and the entity
carries no public field: `Proposer`, `Reviewer`, `Verdict`, `DiffDigest` and the
state are all `readOnly:"true"`, and there is no `Face` and no public route. A
proposal is an internal object about an internal write; an anonymous visitor has
nothing to read here and nothing to propose.

### The operator boundary

None of the three keys is the operator's (`module.Permission.Operator` is false for
all three), and the module registers no `s.Ops` route. What an operator does about
change control is ordinary: `modules/audit` records the four transitions like any
event, and the trail's own operator surface answers for reading them. The
installation's decision about *which* writes need a proposal is configuration
(`flags:` in `kit/config`), which an operator changes by deploying, not by asking.

### Provisioning

Nothing is created on first boot beyond the table: no seed row, no default
proposal, no synthetic history. A tenant with no `change:decide` holder cannot
approve anything, which is the configuration a customer's own roles decide — and
that configuration is what the rule is for, so the module seeds none of it. The
three keys are grantable the moment the module is composed, and
`modules/change/messages/pt-PT.json` carries the words a refusal uses for a tenant
served in Portuguese.

## The table

`modules/change/migrations/000038_change.up.sql`, owner `change`, with no `Adopts`
(nothing ever applied these bytes under another owner, and the upgrade fixture only
claims the files that predate modules owning their SQL — see
`modules/change/migrations.go`) and no `RulesFrom` (every file is guarded). Subject
is `(subject_module, subject_entity, subject_id)` with no foreign key — the reason
`modules/task` already gives for `Source` — and the
partial unique index over `(tenant, subject, diff_digest) WHERE state IN
('proposed','approved')` is what makes a second submit of the same diff the same
proposal rather than a race, which `internal.Service.oneOpinion` then answers as
the row to its own author and as a conflict with no row to anybody else. That index
is enforced by Postgres, which means the loser of two simultaneous submits gets
`23505` *and* an aborted transaction: `internal.Service.insert` therefore runs the
insert inside a savepoint, so the loser can roll half of it back, read the winner's
answer with it. Without the savepoint the recovery read would be a statement in a
transaction that refuses statements, and the person who clicked twice would get a
conflict and no proposal id. ENABLE / FORCE ROW LEVEL SECURITY plus
`platformkit_tenant_match(tenant_id)` on both sides; the version is 38 and adopted by
nobody, because it sits above the highest file the foundation itself shipped, so the
legacy-layout fixture never claims it and there is no row for an adoption to re-own
(see `migrations.go`).

`diff` is `text` and not `jsonb`, with a CHECK on `jsonb_typeof(diff::jsonb) =
'object'`. jsonb stores a number as a numeric: it rewrites `1e2` as `100` on the
way in, and the bytes that come back then digest to something other than the
`diff_digest` in the neighbouring column. A verdict is about the exact bytes, so
the column holds the kind of value that gives back the bytes it was handed, and the
CHECK is what jsonb's own typing used to provide.

## Mobile (decision 0019)

A proposal is a person's object, and a device is where a decider notices one. What
this module does not do is decide what that looks like: it registers no
`rest.Spec`, so it has no catalog resource and no generated screen, and the
`Interface`/`Verification` answer decision 0019 asks for is therefore a
follow-up's — the four commands are `POST /api/v1/change/proposals` and
`/proposals/{id}/{review,apply,withdraw}` on the app surface, which is enough for a
catalog resource with the four as catalog commands and a derived form, and
`make mobile-e2e` has no flow for them until somebody composes one. What is true
today is stated rather than implied: nothing in this delivery was verified on a
device, because no device-facing surface asks for one.

## Limits

`modules/site`'s settings row is the only subject composed, in the reference
application, which makes `PUT /api/v1/site/settings` the only write change
control routes there. A subject nobody binds is `ErrUnsupportedSubject` — the
list in `apps/platformkit` is what makes a subject exist, and the one gate is
one flag (`change.control.site-settings`), off by default, one boolean for the
installation rather than per tenant; unreadable, it refuses the write.

Also not here, each one named rather than hidden: the `changetest` fake and the
C1–C14 conformance suite the specification lists; `modules/change/...` *is* in
`Makefile`'s `RACE_PACKAGES` and `concurrency_test.go` is the two-applies case
that uses it, and no other case in this module runs under `-race` by default;
`modules/content`, `modules/billing` and every other module's rows are not
subjects; there is no review queue, which is a screen and therefore belongs to
whoever composes one; `contracts.Diff` covers RFC 7386 and nothing else — an array
is replaced, never merged, because merge patch has no rule for one and inventing
one here would be a patch format of our own; and there is no way to re-open a
declined proposal, which is what proposing it again as a new row is for.
