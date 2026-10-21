# Operation receipts and HTTP retry memory

The retry store remembers one tenant/caller/method/route/key binding and its
request fingerprint. A tracked claim additionally fixes an immutable operation
reference and admission instant. Taking over an unbegun lease preserves those
facts. Existing unlinked rows remain historical, without invented receipts.

`Operations` reserves immutable facts and reads or transitions the minimal tenant
receipt through `db.Tx[db.Tenant]`. SQL uses FORCE RLS and `crud.GetForUpdate`;
Memory applies the same outcome decision without promising transaction rollback.
The storage column `id` is the occurrence reference, exposed as `ref`, so the
existing aggregate-root lock can be reused. The retry table remains system-only.

An empty outcome means unknown. Known outcomes are `succeeded`, `failed` and
`pending_approval`. Unknown can reach any known outcome; approval-pending can
reach succeeded or failed. A terminal result cannot change. Every changed
outcome increments revision. A stale expected revision, invalid outcome or
backward instant returns an error and no row. Repeating the current outcome at
the current revision changes nothing. Instants are explicit UTC microseconds.

`events.RecordOutcome` couples a changed result with its typed
`platformkit.operation_outcome` outbox event. `events.Succeed` stages a domain
owner's successful result before its successful event. A rolled-back mutation
keeps neither state nor its domain events. Its root boundary records failed
kernel evidence only after a definite rollback. Commit/network uncertainty
never creates failure evidence. HTTP retries resolve it through the command effect
fence: only an existing unmarked fence permits another execution after the lease.
A known receipt outcome is spent even if its answer was lost. Non-HTTP command
roots retain their conservative begun-claim refusal; they do not write HTTP markers.

`kit/app` admits every unsafe HTTP method and supplies SQL storage and a clock.
The response's `Operation-ID` is the key token or a generated UUID string.
`Operation-Location` identifies the immutable occurrence for authenticated people.
The signed-in `GET /api/v1/app/operations/me/{ref}` projection compares the
initiating person inside the request tenant transaction. Other people's, other
tenants', expired and absent receipts all return 404. Unknown returns 202 with
`Retry-After: 1`; known returns 200. Replies are private and no-store.

An async owner explicitly calls `request.DeferOperation`. Delivery carries the
origin without restoring a user session; only an owner callback records actual
completion. A subscription's optional `Terminal` callback records definite
failure atomically with its dead letter and delivery claim. Ordinary consumers
and broker acknowledgements never complete an intent. A terminal snapshot
from a validated cross-tenant delivery can be copied as causality in the target
tenant; it cannot grant access to, or transition, the originating receipt. Historical events clear
inherited causality; `events.RunOperation` starts a command under the existing
delivery claim, with a stable event-derived key and fingerprint for retries.

`events.RunOperation` is the root for in-process tenant commands. The task SLA
sweep uses it per tenant command. `app.AdmitCommand` links the seed CLI to its
existing authoritative system transaction after its actor checks. A new tenant's
provisioning child uses `events.ProvisionOperation` inside its creator's transaction;
until creation commits, that tenant and child have not been accepted.

The change module links a committed proposal to its origin. Approval keeps that
origin pending; application succeeds it, decline or withdrawal fails it. Reviewer
and Apply commands retain separate receipts. The current actor remains the
reviewer/applier and the operation person remains the original proposer.

Answer replay expires after 24 hours from initial admission. Unknown and
approval-pending receipts retain their key identity after that window, returning
spent. Terminal receipts and keys expire at the later of admission/outcome plus
24 hours. Purge rechecks eligibility under key-then-receipt locks. References are
never redirected when a token is reused; outbox and audit history do not cascade.

## Composition

**Reused** — request contexts, retry Claim/Begin/Complete, tenant transactions,
CRUD root locks, the transactional outbox, delivery claims, change proposals and
audit reads carry the existing write path.
**Added** — the tenant receipt and its person projection because HTTP answers
and request IDs cannot represent an authoritative operation outcome; portable
`kit/operation` values keep transports independent of request and storage.
**Made reusable** — one SQL/memory outcome decision and explicit command,
approval, async-completion and terminal-evidence mechanisms for later consumers.

## Limits

No list, cross-person read, client outcome setter or generic abandonment timeout
is provided. Unknown external results require owner reconciliation; provider
idempotency remains the provider owner's responsibility. Old publishers and
unlinked retry rows supply no completion proof. Migrate before deploying new
publishers and close that rollout window before requiring bound completion.
If schema expansion paused at a data migration, drain it and migrate again
before admitting writes that require the receipt table.

HTTP effect fences commit with domain writes. Claim holds the key row and resolves
the fence under its transaction advisory lock; takeover replaces the fence owner,
so a stale handler must roll back. Missing fences from before this protocol do not
prove rollback. Purge retains a fence while its retry identity remains, including
unresolved operation receipts. Memory cannot undo a marker after a real rollback.
