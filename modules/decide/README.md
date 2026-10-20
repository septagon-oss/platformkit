# Decide

Decide owns a tenant's questions, options, participants, changeable ballots and
ranked results. Its [SQL service](internal/service.go) and the conformance fake
call the same [ballot decisions](contracts/decision.go). The shared
[conformance cases](contracts/decidetest/conformance.go) run against both; the
SQL suite additionally exercises RLS, concurrent ballots, outbox rollback,
signed credentials, deadlines and notification delivery claims.

This is an implementation checkpoint. The remaining integration and verification
limits below are explicit; a passing module suite does not establish every
accepted contract boundary.

## Reuse and boundaries

The existing consumer is Content's deck in [module.go](../content/module.go),
through [rest.Spec](../../kit/rest/rest.go), the
[resource adapter](../../kit/rest/screens.go) and
[deck projection](../../ui/screens/hints.go). Task's
[service](../task/contracts/task.go) and
[conformance fixture](../task/contracts/tasktest/conformance.go) supply the
organization, tenant transaction and test harness precedents. Work's project
membership and revision contracts were inspected as historical comparisons;
Work is not a dependency. The foundation's module graph has no catalogue pin.
The inspected catalogue has quiz/screening questions but no group ballot owner.
The old shelf's review_queue owns terminal operator approve/reject/defer and
producer dispatch, rather than one changeable ballot per person/option. Its
registrar and older gesture previews are not copied: the foundation already owns
the deck. Existing identity, file and notice capabilities remain their owners'
work. Searches are bounded to the trace inventory, catalogue module Go/README
files, and the four old-platform repositories' Go/README files.

| Deliverable | Reuse decision |
|---|---|
| Question, option, participant, ballot contracts | **new, because** the trace inventory found no group-decision domain in the foundation, catalogue or old-platform shelf; **composed from** `entity.Base` for identity/tenant/timestamps. |
| Commands, authorization and revision decisions | **new, because** Task assignment, Content publication and Work items do not own votes; **composed from** `db.Tx[db.Tenant]`, locked CRUD and the existing operation receipt boundary. |
| Ranking, voter groups and live match predicate | **new, because** the inventory found no scorer or group-ballot query. |
| Atomic domain events and match notification | **composed from** `events.Declare`/`Publish`, outbox delivery claims and Change's `Notifier` subscription pattern; **new, because** Decide's three payloads have no existing domain owner. |
| Signed revocable admission and undo claims | **new, because** file URL signatures, personal bearer tokens and password invitations have different scope and revocation contracts; standard-library cryptography, no new library or token registry. |
| Four-direction deck and generated forms | **composed from** `entity.DeckHints`, `rest.Spec`, `rest.Command`, `httpx.Resource`, `screens.Describe` and `ui/resource`; extend those owners for service reads and authoritative command responses. |
| Images and use tracking | **composed from** `present:image`, `rest.FileUses`/`FileShown` and File's existing use ledger. |
| Composition | **composed from** `pkit.NewModule`, typed Needs/Provides and `pkit.NewApp(...).Use(...)`; no other wiring path. |
| Migrations and real fixtures | **composed from** module-local `db.MigrationSource`, forced RLS and `dbtest.Schema`; **new, because** four Decide tables do not exist. |
| Conformance fake and cases | **composed from** `porttest.Fake`, `Store`, `Do`, `Clock`, `Grants`, `Recorder` and Task's harness shape; **new, because** the inventory found no Decide decision cases. Reuse User/Notification fakes for their collaborators in SQL integration tests. |
| Copy and later journeys | **composed from** `resource.WordsFor`, module catalogues and `e2e/steps/kernel.ts`; later clients own motion and pages. |

Decide is a kernel capability. It imports no catalogue or higher-tier module.
Its contracts use kernel values and the existing UUID value type; they name no
HTTP framework, storage driver, SDK or HTML. Collaborators are Decide-owned
ports implemented at composition, or existing kernel file/tenancy ports.

The declared `decide.Module` provides `contracts.Service` and needs
`contracts.Access`, `contracts.Clock`, `contracts.InviteKey`,
`contracts.Notifier`, `jobs.TenantLister`, `rest.FileUses`, `rest.FileShown`
and `richtext.Files`. The last port validates that an option names an accessible
workspace image; file-use bookkeeping alone does not establish its media type.
There is no optional authorization or notification fallback, no contribution
collection and no branch on another module's selection. Its one wire function
constructs the service, Specs, subscription and job from these typed values.
`SweepEvery` is a constructor option read by the job, not an ignored manifest
field. Applications compose this one module via NewApp.Use; no registry is edited
in specify. Required adapters are supplied by the composition using existing
identity/file/notification owners. User.Get and Auth.Permissions alone do not
promise commit-lifetime revocation locks: an owner-supplied locked identity/grant
adapter is an explicit integration obligation before implementation can claim
the Access contract. An application may not read another module's tables to
pretend that obligation is fulfilled.

## Identity, fields and relationships

All IDs are UUIDs. Every stored row embeds `entity.Base`; the transaction stamps
its tenant and the caller cannot submit it. All four tables have a tenant column,
forced RLS and tenant-qualified uniqueness/foreign keys. A person is the existing
workspace identity, never an account created by Decide. Creator and person IDs
are immutable and checked through Access in the same tenant transaction.

| Entity | Fields and meaning |
|---|---|
| Question | `id`, `title` (trimmed, 1–200 Unicode code points), `description` (plain text, at most 10,000), optional `closesAt`, `allowLinkJoin` (default false), `status` (`open` initially, `closed` terminal), `creatorId`, positive `revision`, optional `closedAt`. Private `inviteGeneration` and `inviteExpiresAt` are read by link issue/revoke/join. |
| Option | `id`, `questionId`, `title` (same bounds), optional `imageId` (`present:image`, private workspace file), optional plain-text `note` (2,000 code points), optional absolute HTTP(S) `link` (2,048 UTF-8 bytes, no credentials), nonnegative `position`, positive `revision`. Private `removedAt` excludes removed options; private `firstMatchedAt` records the lifetime event latch. |
| Participant | `id`, `questionId`, `personId`, `joinedAt`, positive `revision` (1; membership cannot be edited or removed in this version). Unique `(tenant_id, question_id, person_id)`. The creator is inserted as a participant on Open. |
| Vote | `id`, `questionId`, `optionId`, `personId`, nullable `value`, `at`, positive `revision`. Unique `(tenant_id, question_id, option_id, person_id)`. A value is `yes`, `strong`, `no` or `skip`. Null means an undone first vote, retained as a revision tombstone; it is excluded from tallies and treated as unvoted by the deck. `at` is the last accepted change time, including undo. |

`Question.revision` changes on close, option add/remove, participant addition and
invite issue/revoke. Votes change only their own revision; a group member's vote
never makes another member's quoted vote revision stale. Options change revision
on removal; recording the private match latch does not change their public
content revision. There is no edit/reorder/reopen/delete-question operation.
Duplicate titles and equal positions are allowed; IDs break ordering ties.
Every state field above has a command or query reader. No stored score, counters,
percentage, per-user queue or redundant current-match boolean is introduced.

Open accepts one or more initial options and creates the question, options and
creator membership atomically. This gives an open question at least one option.
Removing the final active option is refused; removing an absent/already removed
option is an authorized no-op, even if the active set is already empty. No
minimum number of voters is imposed beyond nonempty agreement for a match.

## Commands and state transitions

Public inputs never contain actor/person for self-service voting or joining.
The service resolves the active actor through `Access.Current` inside the supplied
transaction. Every public mutation requires a current read grant plus the grant
and object scope in the permission table. The HTTP guard is an additional gate.

| Command | Input and authoritative effect |
|---|---|
| `open` | Question title/description/closesAt/allowLinkJoin and initial option definitions. No existing revision; new rows start at 1. Creator is the actor. A supplied closing time must be strictly later than the locked decision clock. |
| `close` | Question ID and `ifRevision`. Creator only. `open -> closed`, `closedAt=now`, increment question revision, emit `decide.closed`. A current-revision close of an already closed question is a no-op. |
| `add-option` | Question ID, `ifRevision`, option definition. Creator only, effectively open. Create an option at revision 1 and increment question revision. Image permission/type checks and FileUses are in the same transaction. |
| `remove-option` | Question ID, option ID, `ifRevision`. Creator only, effectively open. Tombstone an active option, increment option/question revisions, release file uses; retain its votes and match latch. Refuse only when this write would remove the last active option. Lookup is qualified by question as well as tenant; a foreign-question or invisible ID is the same absent no-op, without an unscoped existence lookup. |
| `add-participant` | Question ID, target person ID, `ifRevision`. Creator only, effectively open, target an active person in this tenant. Insert unique membership and increment question revision; an existing membership is a no-op after the same authority/revision checks. |
| `issue-invite` | Question ID, `ifRevision`, explicit `expiresAt`. Creator only, effectively open, link joining enabled. Expiry must be strictly future and no later than closesAt if present. Increment generation and question revision, replace expiry; return the signed token only to this caller. Reissuing invalidates every older generation. |
| `revoke-invite` | Question ID, `ifRevision`. Creator only, permitted after close. If the stored invite expiry is nil, no-op. Otherwise increment generation/revision and clear expiry, including an already expired link. Revocation never removes participants already admitted. |
| `join` | Question ID, signed token, `ifRevision=0` for absent membership. Validate signature and the named question binding before locating its question; then lock/recheck tenant, epoch, expiry, enabled joining and effective openness. Insert the current actor's membership and increment question revision. An existing actor membership at revision 1 is an idempotent no-op only after all token/admission checks. A link grants no workspace role or permission. |
| `vote` | Question/option IDs, `value`, `ifRevision` of this actor's ballot (0 for physically absent). Participant only, effectively open, active option. Insert/update this pair, stamp now, increment its revision; emit `decide.voted`. See retry and match rules below. |
| `undo-vote` | Opaque undo token. Recheck current vote grant, participant membership, effective openness, option and token scope, and compare the token's post-vote revision. Restore the previous value or null, increment revision and stamp now. Publish `decide.voted` with the restored nullable value. An exact replay of the immediately completed undo is a no-op; any intervening vote makes the token stale. Undo never resets a match latch or retracts a notification. |

`ifRevision` is a caller-owned input name; stored `revision` is server-owned.
Non-voting commands compare question revision exactly before deciding, including
no-ops. A changed revision is a correctable conflict; reload and send a new intent.
There is no implicit retry with a freshly loaded revision.

A vote with the same value at the current revision succeeds without writes or
another event. Replaying a vote at the immediately preceding revision also
succeeds without writes only if the stored value is identical: this is the
natural idempotent upsert of the same person/option/value. Any other stale revision
is refused, including an old vote after a value changed away and back. Authority,
parent state and scope are checked before this replay rule. Other exact HTTP
retries use the existing kernel operation receipts and original intent; Decide
adds no generic retry journal. Receipt recovery rechecks current authority and
returns no retained result to a revoked actor. Unknown commit outcomes are
observed under the same operation identity before a new intent is issued.

`closesAt` is an admission deadline: equality is closed. A late vote/join/structural
command is refused without lazily changing the question or writing an event.
A module-owned deadline job enumerates tenants using the existing TenantLister,
opens one `db.Tx[db.Tenant]` per tenant and closes due questions under the same
question lock. It emits `decide.closed` once, with reason `deadline` and null
actor. It is an internal scheduler operation, not a route or an implicit promotion
of an anonymous caller. Its conditional write reads the locked current revision.
A manual close racing the job wins or observes a closed no-op. Reads expose
`acceptingVotes=false` at the deadline even before the job materializes `closed`.
`Deps.Clock` is required; `SweepEvery` is read by the job (default one minute),
so event delivery can lag the deadline while mutation admission cannot. Zero
selects the default interval; a negative interval is refused before startup.

## Permissions

Permissions declare capability, never tenant ownership or creator/member scope.
There is no administrative bypass of creator-only commands. Product roles grant
these keys; Decide creates no roles and contains no client or sector policy.

| Operation | Required current grants | Object scope |
|---|---|---|
| Discover/read/list questions | `decide:read` | Creator or participant; list/count are filtered before pagination/counting. |
| Read options, people, results, deck, export | `decide:read` | Creator or participant of the named question; deck additionally requires participant plus `decide:vote`. |
| Open | `decide:read`, `decide:manage` | Active workspace person; becomes creator/member. |
| Close/add/remove option, add participant, issue/revoke invite | `decide:read`, `decide:manage` | Current creator of this question. |
| Join | `decide:read`, `decide:vote` | Active workspace person and valid tenant/question admission token. |
| Vote/undo-vote | `decide:read`, `decide:vote` | Current participant; ballot person is always the actor. |
| Deadline close | No user grant | Only the module's privately constructed scheduler, within each explicit tenant transaction. |
| Match notification subscriber | No user grant | The existing event consumer's tenant transaction and delivery claim; recipients come from the match's roster snapshot. |

Access resolves current account status, role grants and any credential ceiling
again after the question lock. Its implementation must retain the identity and
grant/revocation locks until commit, in the identity owner's canonical lock order.
A cached request Principal is an identifier, not proof that its grants remain
current. Missing Access is a composition error; its outage refuses the call.
The Access call receives the complete target-person set (only add-participant
has an extra target), checks it in the same tenant, and acquires all identity
locks together in owner order. It never locks an actor and then discovers a
lower-ordered target lock while another command can do the reverse.

`Questions` accepts optional stored status, offset (default 0) and limit (default
50, maximum 200). Negative offset, negative/oversized limit or any other status is
an invalid-input refusal (**correctable**). It orders by createdAt descending then
UUID ascending. Its total and page use one SQL statement snapshot, with creator/
participant visibility applied before both counting and paging. `Options` returns
all active options ordered by position/ID; `Participants` returns all question
members ordered by person ID. Both use the question's read authority and share
lock; neither exposes generic tenant-wide child lists. Questions that close
between list requests may change later pages; no cross-request snapshot is
promised. Unsupported filters/sorts are refused (**correctable**), not ignored.

## Transaction, concurrency and failures

The caller owns the tenant transaction and must roll it back on every error.
Decide opens no second transaction inside a command. Resolve `db.TenantOf(tx)`
and reject missing/mismatched request scope before data access. Each mutating
command locks the question first, then re-resolves authority, then locks children
by UUID order. Do not acquire a child and subsequently wait for its question.
Identity adapters must not acquire Decide locks, preventing a reverse lock edge.
Aggregate readers take the question's share lock before current Access and child
reads, holding it through the query; mutation exclusive locks give one consistent
results/deck snapshot. A question-only read/list uses a single nonlocking SQL
statement for rows plus visibility, under retained current-authority locks; it
never takes a question lock after an identity lock.

The question lock serializes voting, undo, roster changes, option removal, link
revocation and closing. This makes both parent-state checks and the once-only
match latch authoritative. A unique ballot index additionally prevents duplicate
pairs; it cannot replace aggregate locking. A stale actor or expected revision
returns a zero result, with no domain, file-use, notification or outbox writes.
Provider/SQL/outbox failure returns zero and rolls back all effects. No refusal
returns a loaded row. Kernel operation failure evidence remains kernel-owned.

| Refusal | Classification and recovery |
|---|---|
| Invalid UUID, enum, text, URL, negative position/revision, absent initial options, missing required input | **Correctable**: fix the input. |
| Missing actor/account, disabled account, absent current grant, or creator/member restriction | **Correctable**: authenticate and obtain legitimate authority or use the owning actor; a token cannot repair a workspace grant. |
| Missing tenant; caller-supplied tenant differs from transaction | **Immutable** for this invocation: resolve an authenticated tenant request; never switch scope inside the call. |
| Unknown or cross-tenant question/option/person; option belongs to another question | **Immutable** for the requested identity/scope. Same not-found shape; no foreign row is returned. Authorized remove-option treats any absence in its tenant/question-qualified scope as a no-op instead, without disclosing a foreign binding. |
| Stale expected revision or stale undo token | **Correctable**: reload; retain the old operation identity until its outcome is known. |
| Question closed or deadline reached | **Immutable** for this question: it cannot reopen or accept later mutations. Close/revoke remain allowed as above. |
| Last active option would be removed | **Correctable**: add another option first. Finding no such option is a no-op, not this refusal. |
| Join disabled | **Correctable** only by the creator's legitimate product flow/new question; this version has no setting-edit command. |
| Malformed/forged token, wrong purpose, tenant, question or person | **Immutable** for this credential/scope; disclose no claims. |
| Expired/revoked/replaced invite | **Immutable** for this token; ask the creator for a new link. |
| Unsupported or inaccessible image | **Correctable**: supply a permitted image in this workspace; no public file grant is created. |
| Access/file/key provider failure, SQL conflict/deadlock, outbox failure | **Correctable** operational failure: retry only after rollback or outcome observation, without dropping a grant check. |
| Missing provider, zero/invalid signing key, nil Clock, negative sweep interval | **Correctable** composition error before resources open. |
| Revision or invite generation overflow | **Immutable** for this identity; refuse before mutation and create a new question. |

Error categories use the existing fault/not-found/invalid/conflict/authorization
mapping at the adapter; the contract's stable reason values distinguish domain
recovery. No special response body contains the stale row. Read refusals return
no counts, voters, file references or existence hints. Internal corrupt aggregate
input to a pure decision is an immutable integrity refusal, never a partial tally.

## Results query, match and events

`Results(questionId)` is read-only, requires the table's authority, and includes
all active options, including zero-vote options. For each option it returns:

- `score` as int64: `2*strong + yes`; no and skip contribute zero.
- `counts` for all four values (zero keys included), and `voters` for all four
  values (empty arrays included). Each group contains distinct workspace person
  IDs in canonical UUID byte order. Display names/avatars are resolved through
  the existing people/presentation owners, never copied into votes.
- `matched = totalVotes > 0 && yes+strong == totalVotes`. A persisted skip vetoes
  agreement just as no does; people who have not voted do not veto. Unanimity of
  all participants and majority thresholds are not alternative module policies.

Order is score descending, position ascending, option UUID byte order ascending.
No alphabetical or arrival-order tiebreaker is locale-dependent. Counts and voter
arrays describe the same locked snapshot. There is no hidden limit or percentage;
a client may derive a percentage only after naming its denominator. The result
also carries question ID, structural question revision, observedAt and
acceptingVotes. Question revision alone is not an ETag for changing votes.

On any accepted vote or undo, compute the resulting option's live predicate under
the question lock. If it is true and `firstMatchedAt` is null, set that latch and
emit `decide.matched` in the same transaction as the ballot and `decide.voted`.
One affirmative first voter is enough; zero votes never match. The live predicate
can become false and true again, but its event remains once per option lifetime.
Removal and undo do not reset the latch. A new option ID has its own lifetime.

| Event | Typed payload and timing |
|---|---|
| `decide.voted` | questionId, optionId, personId, previous nullable value, new nullable value, voteRevision, at. Every actual vote/undo change; silent on no-op/refusal. |
| `decide.matched` | questionId, optionId, agreeing voter IDs, participant recipient IDs, at. Once at the first nonempty agreement; both ID arrays sorted and deduplicated from the locked snapshot. |
| `decide.closed` | questionId, questionRevision, nullable closedBy, reason (`creator`/`deadline`), at. Once on the persisted open-to-closed transition. |

Tenant, event ID and causality are the existing event envelope's responsibility.
No join token, signing key or option description goes into an event. Only these
three events are declared; no generic CRUD writes are exposed, so invented CRUD
events are unnecessary. Subscription delivery is at least once. The match handler
uses the existing per-(event, durable) delivery claim and writes all participant
notices through a required `Notifier` port in that consumer transaction. Failure
rolls back the whole notice batch/claim and retries it; successful redelivery is
silent. Participants joining after that snapshot do not receive historical
matches. The adapter uses Notification.Notify with a stable event intent, a
minimal reference and product-provided protected link; it performs no provider
network call in the voting transaction. Push/email availability cannot roll back
a vote that has already committed.

## Admission and undo credentials

`Deps.InviteKey` is a required typed secret of at least 32 random bytes supplied
by application composition. New validates it and constructs the signer; no new
unread configuration namespace is added. A changed key invalidates outstanding
invite and undo tokens. Products own secret loading/rotation; no environment
value or key is recorded in this specification. No remote signing provider is
required. The standard library supplies HMAC-SHA256, secure randomness and
constant-time MAC comparison.

Invite wire form is unpadded base64url(payload) + `.` + base64url(mac). Payload is
canonical UTF-8 `v1/tenantUUID/questionUUID/generation/expiryUnixSeconds` (lowercase
canonical UUIDs, positive base-10 integers without leading zeros). MAC input is
`decide.join` followed by NUL then the payload bytes. Reject noncanonical forms,
extra fields, tokens longer than 1,024 bytes and non-32-byte MACs. Signed expiry
must equal the question's stored expiry, and generation must equal its current
positive epoch. `now >= expiry` is expired. Store no raw token. A question read or
export never reconstructs one. The token is submitted in a POST body; product
share links put it in a fragment, so redirects/referrers/access logs need not
carry the credential. Identity onboarding is completed before POST join.

An undo token uses the same base64url/MAC framing with purpose `decide.undo` and
payload `v1/tenantUUID/questionUUID/optionUUID/personUUID/postRevision/previousValue/expiryUnixSeconds`.
The same UUID/integer canonicalization applies; previousValue is a stable vote
key or `-` for null. Expiry is 15 minutes from the accepted vote, capped at
closesAt and floored to whole seconds. An already reached floored expiry omits
the token; clients offer rewind only when a live token was returned. Its value is an opaque command
response field, never a list field. Rewind restores that prior value; null restores
the unvoted queue position. Keep ballot tombstones so a later cast cannot reuse
an old revision. The immediately completed undo retry is recognizable by
revision=postRevision+1 and the restored prior value; all other revisions refuse.
Current authorization and time are checked even on a recognizable replay.
Undo produces no further undo token, preventing a rewind loop. Normal vote
editing remains available without a token while the question accepts votes.
Same-value no-ops also omit a new token. Vote and invitation responses declare
credentials: the kernel retains no answer, and a keyed retry is spent (409).

## Deck and generated surface integration

`Deck(questionId)` requires participant/read/vote authority and returns only active
options whose actor ballot is absent or null, ordered by position then ID. It
returns total active options, number with a non-null actor vote, and remaining
queue count from the same snapshot. Every option carries the actor ballot's
current revision (0 for physically absent) so voting after undo quotes its
tombstone revision. No client-supplied person/filter may change this scope.
A closed/effectively closed question returns an empty queue plus progress and
`acceptingVotes=false`; ordinary option/results reads remain available.

Declare four eligible row commands through the existing `rest.Command` owner:
right `yes`, left `no`, up `strong`, down `skip`. Each fixes one value and delegates
to the same vote service. Down is a persisted vote, never `DeckDirection.Skip`.
Card title, note/link summary, `present:image` cover and position ordering come
from the existing entity/presentation metadata. Rewind delegates to undo-vote.

Three extensions are implemented in the existing foundation owners. Their
accepted behavior is described here; verification limits remain below:

1. `rest.Spec.Source` supplies a typed service-backed read adapter for list/get/count,
   shared by HTTP and `httpx.Resource` generated screens. Both option scope and
   question visibility must run through that adapter before pagination, count,
   file projection or summary. The actor's queue is a read projection with
   `unvoted=true`, not a stored flag or an interpolated literal person filter.
   The options Spec exposes this read-only projection and has no CRUD writes.
   The read projection embeds Option and adds read-only, nonstored `unvoted`
   and `voteRevision` scalar fields, so the existing entity schema stays flat.
   Its scalar deck queue is `unvoted:true`; declared fixed ordering is position
   then ID. Every advertised filter/order either reaches the service adapter or
   is refused, never ignored. General option lists are a separate scoped read.
2. Extend existing command responses and `httpx.Command` execution to retain
   typed result/undoToken metadata in both generated and JSON adapters. Extend
   the existing deck/catalogue with `undoable:true` only when every advertised
   vote direction names an authoritative undo command and can return its token.
   The token is sent to that command, never inferred by a client. The native
   catalogue and browser use the same response metadata; no second schema or
   module HTML renderer is added. The existing golden rejecting unsolicited
   undoable metadata must be deliberately extended with positive/negative cases,
   including authorization filtering and zero-value compatibility.
3. Current deck validation accepts only commands invocable with empty/default
   input. Add row argument bindings to the existing command declaration and
   catalogue: `questionId` takes the card's `questionId`, and `ifRevision` takes
   its `voteRevision`. These are copied from the observed card into the request,
   never refreshed to the latest revision while a command runs. Store bindings
   behind a pointer so CommandOptions retains comparability, validate argument/
   scalar-field names and compatible types before mounting, and require every
   nondefault argument to have a binding for deck eligibility. Generated forms,
   browser and native command clients use the same declaration. Missing/invalid
   submitted values are correctable input refusals. They cannot supply an actor
   or tenant, and a stale bound revision is still refused by the vote service.
   A default-zero revision is insufficient after first-vote undo. Tests must
   pin this case and demonstrate that binding does not silently refresh a stale
   card or bypass normal command input validation.

The served-deck SQL test exercises the queue, catalogue, fixed directions,
authoritative vote response, signed rewind, tombstone revision and stale card
refusal. Source callbacks serve both JSON and generated resource closures;
`CommandOptions.Bindings`, `CommandResult`/`Response`, `httpx.Command.Execute`
and the catalogue carry observed inputs and authoritative outputs. Web/mobile rendering,
buttons, keyboard behavior, reduced motion, match animations and later Decide
screens belong to their client tasks. Generated no-motion forms continue to come
from rest.Spec; this module adds no pages in this phase.

## Storage, retention, time and validation

[000073_decide.up.sql](migrations/000073_decide.up.sql) adds the four tables,
transactionally, through the module-owned embedded MigrationSource. It follows
the existing kernel version 72. There is no down file or applied-file rewrite.
Use `decide_questions`, `decide_options`, `decide_participants`, `decide_votes`.
All have `ENABLE ROW LEVEL SECURITY`, `FORCE ROW LEVEL SECURITY` and
`platformkit_tenant_match` in both USING/WITH CHECK. Foreign keys and uniqueness
include tenant ID; votes additionally reference the option's question and the
participant's question/person. Options and votes cannot be reassigned. Check
constraints enforce statuses/values, positive revisions, nonnegative positions
and valid private epoch/null-expiry pairing. Index the tenant/question ordering,
unique participant pair and unique ballot tuple. SQL is not generated here.

Removed options and ballot tombstones are retained; there is no automatic purge,
participant removal or historical-vote append ledger. Current ballots and rankings
freeze after close; query observation metadata and invite revocation may change. Tenant deletion follows the existing tenancy owner's teardown
policy. Decide offers an authenticated read export of the four entities/current
ballots plus results, under the same question share lock and permission as Results;
it excludes secrets, credentials and internal latch fields. Export is JSON with
stable UUID/time/value representations and no new third-party format library.
Products own retention duration, subject-erasure policy, external archival and
any future legal hold; they cannot bypass current scope checks with an export.
A product requiring subject erasure needs a follow-up module contract; this
version provides no subject-erasure command or permission to delete its tables.
No anonymity or secret ballot is promised: participants can see who voted what.

All domain time comes from required `Deps.Clock`, sampled after authoritative
locks, normalized to UTC microseconds. API timestamps require an explicit offset;
local timezone display belongs to the product. Timestamp storage/claims must
round consistently (invite expiry to UTC whole seconds before validation/storage).
User text is UTF-8 plain text; reject invalid UTF-8, NUL and disallowed controls
(tab/newline are allowed in multiline description/note). Trim titles before bounds,
leave other text unchanged and escape it through existing renderers. URLs are
validated but never fetched by Decide. Images use existing File permission/type
checks and usage/visibility bookkeeping; admission never makes them public.
No money or currency is present. No specialist facts, jurisdiction, provider API,
recommendation engine or content truth validation belong to this capability.
Stable enum/command/grant keys are locale-independent; labels use existing module
catalogues and locale fallback, never translated storage keys.

## Executable and remaining conformance

The decision fake reuses porttest storage, clock, grants and event recording.
It delegates ballot acceptance, join admission, ranking and queue membership to
the same pure decisions the SQL service must run under its locks. Its verified
claims model the signer's output; it does not assert signature verification.
The shared cases use independent expected scores, voter sets and snapshots;
a refused decision must return zero and leave both state and events unchanged.

The SQL implementation must run these cases through a real tenant-transaction
harness and additionally prove: RLS as the app role; concurrent inserts for one
ballot; two voters concurrently producing one matched event; vote/close,
join/revoke and vote/remove-option races; rollback after file-use/outbox/notice
failure; retained authority locks versus account/grant revocation; automatic
close at equality; HTTP/generated adapter parity; private results/list/count/export
scope; signature expiry/purpose/tenant tampering; first-vote undo/ABA; served
deck/catalogue and authorized direction filtering. Reuse dbtest.Schema and the
existing journey identity/tenant steps, not another fixture or service launcher.
A fake's tenant partition is not evidence of PostgreSQL RLS.

## Limits

Decide chooses nonempty agreement among actual voters, fixed 2/1/0 scoring,
creator-only administration, authenticated existing-person joining, one active
invite generation, current visible ballots and participant-visible voter IDs.
Products choose question/option content, workspace roles, onboarding, secret
loading, locale/branding, protected page URLs, notification channels, retention
and specialist validation. None is an unanswered module invariant.

Open/add-participant/issue/revoke/undo and internal deadline close are explicit
splits needed to make the decided requirements operable. No brief event or vote
value was dropped. No application composes this module yet, so it remains uncomposed rather than
being specific to one client. No client renderer or dependency pin is changed;
source budgets are priced separately from implementation. The task identifier is not an import path:
the intended module lives at `modules/decide`; literal phase commands aimed at
`modules/T-0393-a-group-decides-together-the-decide-module` name no package.

## Library evidence

Only the already pinned UUID value library is named by the contract sources:
`github.com/google/uuid` v1.6.0, BSD-3-Clause, compatible with Apache-2.0.
Read its cached `LICENSE` at the resolved module directory; it requires copyright,
conditions and disclaimer retention and prohibits endorsement without permission.
No new dependency is proposed. Registry response from
`curl -fsS https://proxy.golang.org/github.com/google/uuid/@latest`:

```json
{"Version":"v1.6.0","Time":"2024-01-23T18:54:04Z","Origin":{"VCS":"git","URL":"https://github.com/google/uuid","Ref":"refs/tags/v1.6.0","Hash":"0f11ee6918f41a04c201eceeadf612a377bc7fbc"}}
```

## Composition

Compose `decide.Module` through `pkit.NewApp(name).Use(...)`, with typed providers
for every port listed above. `decide.WithSweepEvery(interval)` selects the same
wire function with a different scheduler interval; zero means one minute and a
negative interval refuses composition. `decide.MatchNotifications` adapts the
existing Notification service and a product-supplied protected local link to the
required match port. Product composition supplies the locked Access adapter; a
cached request principal or an ordinary user lookup cannot satisfy its contract.

**Reused** — `entity.Base`, `crud.GetForUpdate`, the request tenant transaction,
forced RLS, `events.Publish` and the existing delivery claim, `jobs.PerTenant`,
`pkit.NewModule`, `rest.Spec`, file image/use ports, Notification.Notify,
`dbtest.Schema`, `porttest.Clock`, the conformance suite and shared generated UI
components carry identity, authority boundaries, persistence and presentation.
**Added** — Decide's four-table aggregate, canonical credentials and scoped
service projections carry group ballots because Task, Content and the inspected
catalogue/shelf owners do not own a changeable person/option decision.
**Made reusable** — service-backed Spec reads, comparable row argument bindings,
authoritative command replies, declared rewind metadata and the SQL conformance
fixture are available to the next scoped resource.

## Remaining checkpoint obligations

Noncredential HTTP recovery calls the declared current-authority check in its
request transaction. Decide resolves Access again, retaining current Read and
Manage/Vote locks through commit; a denial returns no cached row, and an outage
refuses recovery. SQL HTTP probes revoke Read, Manage and account activity before
retrying a completed close. Invite and vote responses declare CredentialExtension;
JSON and generated vote-form probes prove their credentials cannot be replayed.

SQL deadline/ballot and concurrent-ballot tests do not establish every requested
vote/close, join/revoke or vote/remove ordering. Current-authority retention is
required of the composing adapter; the fixture adapter uses row share locks but
has not yet been raced against revocation. Browser/mobile journeys, generated
command outcome design-floor measurements and a real user/notification product
composition are still unverified. These are delivery obligations rather than
alternative domain policies. Source-budget approval and repository checks are
reported separately; no verification of these remaining boundaries is inferred.
