# User module

## Composition

`user.Module` consumes independent `Granting` and `Administration` ports supplied by
`auth.Grants`; it does not need Auth's sessions service. `New` refuses nil granting
and an empty `GrantingFunc`. An initialized function adapter remains supported.

**Reused** — the existing command service, administration lock, conformance suite
and transactional outbox continue to own account state and last-administrator protection.
**Added** — provider-owned [capabilities](contracts/capabilities.go) project lookup,
registration, provisioning and command operations without duplicating account logic.
**Made reusable** — each consumer can construct from its required operations while
the deprecated `Service` aggregate remains available to existing callers.

`modules/user` is the people in a tenant: one entity, a `rest.Spec` at
`/api/v1/user/users` guarded by `user:read` and `user:manage`, invitations at
`/api/v1/user/invitations`, explicit lifecycle commands, and pending
registrations for compositions that want operator approval (`user:approve`
guards the approve-registration command). A user belongs to a tenant by
carrying its id, which row-level security matches on, so the module asks the
tenant module for nothing. It takes one dependency, and it is required:
`user.Deps{Administration: &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles}}`.

## The floor under a tenant's last administrator

Three writes used to lock a tenant out of its own administration, each of them a
click on the generated user screen: setting the sole administrator's roles to
none, deactivating them, and deleting them. Afterwards nobody **inside** the
tenant can change a role again — the roles screen, `GET /api/v1/auth/roles` and
`/api/v1/user/users` all answer 403.

Who can still repair it is worth stating exactly. For a customer's tenant the
installation's operator can, with `POST /api/v1/tenant/tenants/{id}/invite`: it
runs in a system transaction and needs no session in that tenant (verified by
running it — 201, and a second person holding `admin`). Two bounds on that: it
hands out a role **by name**, chosen by the application's adapter, so it does
not recover a tenant where that named role is itself the one that was emptied;
and it exists only where an application wires the capability. For the
**operator's own** tenant there is nobody above it — the route declares
`tenant:manage` as an operator permission, held through that tenant's own roles
— so the control plane shuts: no tenant can be created, none can be given an
administrator, and the price list cannot be read. What is left there is SQL.

All three now answer 422 naming the person and the role.
`contracts.CheckedAdministration` is the rule — and it is the rule for
`modules/auth` too, which is what changed here. It answers one question with two
sets of people: the ones who could sign in and administer this tenant before the
write, and the ones it would leave. `contracts.Reach` carries each state, and
both sides are decided by `User.CanAdminister` — `User.Administers` is the wider
predicate and stays the door's own cheap gate, so removing somebody who has not
accepted their invitation is still a write the floor looks at, while somebody who
has not accepted theirs cannot hold the floor up. What is refused is the write
after which nobody who can sign in would hold a role that still grants the
permission; a grant is never refused, so the refusal always has a way out and the
message names it. `internal.Service.floor` takes `pg_advisory_xact_lock` on the tenant
so two administrators standing down at once cannot both pass.

That lock is shared with `modules/auth`, which guards the same property from the
other side: `"administration/<tenant id>"` through `hashtextextended(key, 0)`,
the same literal written out in both modules rather than exported from one, and
pinned by a test on each side and by
[`apps/platformkit/administration_lock_test.go`](../../apps/platformkit/administration_lock_test.go),
which is the only one that can watch a write in one module queue behind a write in
the other. Both halves matter — the same string through `hashtext` is a different
lock and nothing reports it.

**The two floors are now one rule, and this file used to say they were not.**
`modules/auth` used to count roles, and a role nobody holds satisfies a count of
roles: create a role granting `role:manage`, give it to nobody, empty the role
everybody holds, and two 200s left the tenant answering 403 to its own roles
screen. `apps/platformkit/administration_lock_test.go` asserted that hole and now
asserts the opposite (`TestTheTwoFloorsComposeIntoOneInvariant`), with the racing
pairs beside it — two administrators standing down at once, two administering
roles emptied at once, and one of each across the module boundary — and
`TestTheComposedCheckJudgesOneTenant` is the case that fails if the people the
rule counts ever stop belonging to one tenant. The join is `contracts.UserReach`
and the holder read is `Service.Holders`; the auth side reaches them through the
`contracts.Users` port it already had, widened by `Holders`, so neither module
reads the other's rows and each asks the other the half it does not own.

What the rule does **not** cover is written out on `CheckedAdministration`
itself.

**A refused write leaves a record of the attempt.** The write itself is rolled
back — that is what the 422 means — so nothing about it would survive anywhere,
and "somebody tried to lock this tenant out" is the line an administrator most
wants to have kept. Each door therefore publishes one event outside the
transaction it was refused in: `user.administration_refused` from
`internal.Service.recordRefusal` (`SetRoles`, `Deactivate`, and either door that
ends a person's holding — the delete hook and the erasure, which share the attempt
name `delete` — say which door in the payload), and `auth.administration_refused`
from the same shape at `modules/auth/internal/roles.go` when `SetRole` is
refused. Both copy this repository's one precedent for an event that has to
outlive a rolled-back request — `modules/auth`'s `recordFailure` for a failed
login: `db.Detached` over the request's context, the connection `kit/httpx` put
on the request, a two-second budget, the refusal logged and never returned.
With no request connection to write on — a job, a harness, a system transaction
— there is nowhere to write and the write is still refused: the rule was never
contingent on its paperwork. `modules/audit` needs no code of its own, because
`SubscribeAll: true` means a module is audited by having emitted an event, and
the row carries actor, tenant, request id and trace context like any other.

`Deps.Administration` answers which of a tenant's roles grant the permission
that can grant every other one back. It is required, and a composition that
omits it panics at `user.New`: who holds a role is this module's table and
what a role grants is the auth module's, so neither reads the other's rows and
the independent `auth.Grants` provider answers both questions from the same role
policy. `Deps.Granting` must be ready at construction; it does not wait for Auth
or permit its `Ask` function to be filled later.

`Provision` is what `platformkit bootstrap` calls to create the first
administrator with a password; the argon2id parameters are constants here.
Consumers import [contracts/](contracts/) — users, registration, password
rules, events, permissions and the [fake](contracts/usertest/) — never
`internal/`. The admin entry is `/app/user/users`.

## The handle, and what it is not

`handle` is the name a person answers to inside one tenant — `@ada`, in a URL, in
a mention, in the column a human reads. Claim or change it at
`POST /api/v1/user/users/{id}/handle`, which publishes `user.handle_set` carrying
both the old and the new name; read it back with `Service.ByHandle`, case-insensitively
like the address. It is `Immutable` on the CRUD routes for the same reason `roles`
is: a name that moved in a silent PATCH is a name that changed hands while nobody
was told, and "who held `ada` before" stops having an answer.

It is **not** the identifier. `id` is a uuid and every foreign key, event subject
and audit row points at it, which is exactly why the handle can be renamed: nothing
has to follow it. Re-keying on the handle would turn a rename into a rewrite of
history and would take a person's own trail with them. [ADR 0014](../../docs/adr/0014-a-handle-is-an-alias.md)
owns that decision, including why uniqueness is per tenant and which names nobody
may claim.

Signing in by handle is a separate decision and is not implemented here: it belongs
to the auth module, which owns what a failed attempt costs, and it has an enumeration
surface of its own.

## Authorization

### Permissions

The manifest in `modules/user/module.go` (`permissions`) declares three keys, whose constants are in `modules/user/contracts/permissions.go`.

- `user:read` guards the generated read routes (`spec.Read`), the screen `user/users` and its nav entry.
- `user:manage` guards the generated write routes (`spec.Write`), the invitation route `POST /api/v1/user/invitations` (`httpx.Permission(contracts.PermissionUserManage)` in `modules/user/internal/handler.go`) and the `handle` command. The commands `set-password`, `roles` and `deactivate` use `rest.CommandOptions{}`, so they fall back to the Spec's write permission, which is `user:manage`.
- `user:approve` guards the `approve-registration` command only (`PermissionRegistrationApprove`).

### Object scope

None. `git grep` finds no `tenancy.Policy` use in `modules/user`. Access is decided by the route grants and by the tenant transaction, which confines every query to the caller's tenant.

### Duties the module enforces itself

- The last-administrator rule. `Service.floor` in `modules/user/internal/administration.go` refuses a write that would leave the tenant with nobody who can sign in and administer it. It is called from `SetRoles` and `Deactivate` in `service.go`, and from `RefuseLastAdministrator`, which is `spec.AfterDelete` for the delete route. About a row that carries invitation history `invitationFloor` (`modules/user/internal/invitations.go`) asks the same rule and counts a delivered invitation as a way back: the invitation commands, the delete hook and `LockForErasure` — the erasure door's read of the retained person — all ask it. It takes a per-tenant advisory lock (`administrationLock`) and uses `contracts.CheckedAdministration` and `otherAdministrators`. A refusal rolls back the transaction, and `recordRefusal` publishes `user.administration_refused` in a detached transaction of its own so the attempt is still on the trail (see the section above).
- `refuseLifecycleOnCreate` (`modules/user/module.go`) is the create hook that keeps roles and password registrations behind their own commands. `spec.Immutable` refuses `status`, `roles` and `handle` on PATCH.
- `ApproveRegistration` (`modules/user/internal/registration.go`) requires a non-nil acting principal, takes the row lock, and only approves a pending registration that has a password. The acting user id is recorded in the event.

The code does not stop an approver approving their own registration.

### Public faces

None. `git grep` finds no `httpx.Public()` in `modules/user`. `Service.RegisterPending` and `RegisterUnverified` exist as service methods (`modules/user/contracts/registration.go`), and any public signup route that calls them is outside this module. The module uses no `kit/limit`.

### The operator boundary

None. No permission sets `Operator: true` and the spec sets no `OperatorRead` or `OperatorWrite`. `user:manage` is not `tenant:manage` (comment in `modules/user/contracts/permissions.go`). The operator reaches a tenant's users only through the tenant module's `invite` route.

### Provisioning

The module does not create roles. A composition grants `user:read`, `user:manage` and `user:approve` through the tenant's roles, using the auth module's roles API. The administrator role holds the wildcard by construction (`auth.SeedRoles`, mentioned in `modules/auth/contracts/auth.go`), which covers all three. `user:approve` can be granted alone, because approval never changes passwords or roles (`modules/user/contracts/permissions.go`). `Deps.Administration` is required and is wired to `auth.AdministeringRoles` (see the README above).

## Composition (T-0184)

**Reused** — this delivery composes `contracts.User.CanAdminister` (the same
predicate the last-administrator floor decides on, so `Holders` and the floor
cannot disagree), `contracts.Administration`/`AdministrationFunc` (the shape the
new `Granting` port copies), `crud.ErrInvalid` and the `user.roles_set` event
`modules/audit` already records with actor, tenant and `Was`/`Now`.
**Added** — `Service.Holders` and `Deps.Granting`: nobody could carry them, because
`crud.Query.Filter` is equality-only so "whose `roles` overlaps these names" did not
exist in either module, and because the `roles` command took the Spec's write
permission and nothing else, which left self-promotion one form away for anybody
who may write a user.
**Made reusable** — the conformance cases for both (`usertest.RunService`, nine of
them, run against the fake and the Postgres service), so the next delivery that
touches who holds a role runs them whether it means to or not; and
`contracts.GrantingFunc`, the same four-line adapter shape as
`AdministrationFunc`, for any other module that must ask "may this caller grant".

## Composition (T-0136)

**Reused** — `contracts.User.CanAdminister` and `Service.Holders` (the predicate
and the read this delivery needed and both already existed for a different
caller), `contracts.Administration`/`auth.AdministeringRoles` for the names, the
tenant's `pg_advisory_xact_lock` key, and the `contracts.Users` port
`modules/auth` already took from this one — widened by one method rather than
replaced, as `ConfirmAddress` was.
**Added** — `contracts.Reach`, `CheckedAdministration`'s before/after shape, and
`UserReach`/`LeavingAdministration`: the composed question had no home, because
two half-rules existed and neither could be asked by the other's module — a rule
in `modules/auth` would not build from `modules/user`, which imports it. And
`EventAdministrationRefused` with `internal.Service.recordRefusal`: the refused
write is rolled back, so without a record of its own the attempt leaves no trace
at all.
**Made reusable** — one decision function every write path calls through its own
door, with the door's clause passed in as a sentence rather than baked in, and
`authcontracts.CheckedAdministration` as the shape of a door onto the rule for
any module that changes what a grant means. The refusal record is reusable the
same way: it is `recordFailure`'s detached-publish shape applied a second time,
so a module that has to record a write it refused has a precedent in the tree
rather than a pattern to invent. The composition cases in
`apps/platformkit/administration_lock_test.go` are *not* reusable in that sense:
they are unexported functions in `package main` test files, importable by nobody,
and `appoint` writes a `users` row in a system transaction, which is a state the
operator's route reaches and a command's route does not.

## Recoverable invitations

The reference composition enables `InvitationCommands`, `InvitationReader` and
`InvitationRetention` beside the existing `Service`; `InvitationTiming` comes from
`auth/contracts.TokenLifetime` and `ResetInterval` and `Clock` supplies UTC decision
time, so a composition without those companions cannot operate managed invitations.
Existing service interfaces are unchanged.

A new invitation stores the canonical recipient, assigned roles, revision 1,
generation 1, expiry and a delivery identity on the existing user, which stays
`invited` until admission commits; the user, roles and the `user.invited` outbox row
commit together. Generic collection creation uses the same owner decision, and
generic PATCH still reaches no metadata, role or status field.

| Operation | Authoritative result |
| --- | --- |
| `POST /api/v1/user/invitations` | 201, pending user and queued mail intent; `Location` names the authorized reading. |
| `POST /api/v1/user/invitations/{id}/resend` | 202; requires `expectedRevision`, advances revision and generation, and replaces expiry and delivery identity. Old links are unusable immediately at commit. |
| `POST /api/v1/user/invitations/{id}/revoke` | 200; requires `expectedRevision`, records revoked and makes the person inactive. A current-revision repeat is unchanged. |
| Existing roles / set-password / deactivate commands | Waiting invitations require `expectedRevision` (query parameter for the existing empty-body deactivate door, JSON member for roles/password). Role edits advance revision without requesting mail; password admission records its provenance; deactivate delegates to revoke. |
| `GET /api/v1/auth/invitations[/{id}]` | Authorized redacted lifecycle and delivery reading; see [auth](../auth/README.md#invitation-credentials-and-readings). |

Expiry is a reading at `asOf`, including equality with the deadline, and moves no
revision; an expired invitation can still be resent or revoked, while acceptance and
revocation are terminal. A stale revision returns 409 with no row and a missing or
nonpositive one returns 422; a resend inside the existing five-minute bound returns
429 with `Retry-After` and does not extend that bound; a backward or unavailable
clock refuses the decision. `Idempotency-Key` uses the kernel's receipt: a retry
recovers the original response, a changed body returns 422, and a new key bypasses
neither revision, email uniqueness nor cooldown.

Commands lock the person before the tenant administration advisory lock, then read
the actor's current active status and grants: every role name must exist, only added
roles need grant authority, and every added grant must fit the actor's own credential,
with a scoped bearer credential locked and rechecked. The existing last-administrator
decision still protects the last appointment. A refused command publishes nothing.

Existing invited rows are adopted in bounded migration batches as generation 0 with a
one-hour compatibility deadline and no invented send, and a live legacy token keeps
its expiry; a remaining row adopts under an authorized command, and reads answer 503
`invitation_history_initializing` until history exists. Pending and expired
invitations are revoked before deletion or erasure, which locks the retained person
before any credential contributor and scrubs the recipient around the terminal
history.

### Limits

This module defines lifecycle and grant boundaries; the product defines roles,
registration policy and presentation. It does not claim inbox delivery or create
a separate invitation store. Trusted registration, provisioning and identity
provider adapters retain their existing ports; managed admission still uses the
same lifecycle decision. Active-member password recovery remains auth's concern.

## Composition

**Reused** — `rest.Spec`, user `Service`, `CheckedAdministration`, `crud.GetForUpdate`,
tenant transactions, auth password tokens, notification direct-mail records and
the transactional outbox carry the lifecycle.
**Added** — invitation metadata and companion commands/readings because a user
status and a password token could not express revocation, revisions or delivery
uncertainty together.
**Made reusable** — pure invitation decisions and `usertest.RunInvitations`, plus
`authtest.RunInvitationLinks`, exercise the same cases against fake and SQL owners.
