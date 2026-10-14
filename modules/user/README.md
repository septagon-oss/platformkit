# User module

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
`internal.Service.recordRefusal` (`SetRoles`, `Deactivate` and the delete hook
name which door in the payload's `attempt`), and `auth.administration_refused`
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
the application joins them — see `usercontracts.Administration` in
`apps/platformkit/product.go`. `Deps.Granting` is the one adapter it accepts
unwired: the answer is the authentication service's, that service is built after
this module because it looks people up here, and the auth module fills the
holder's `Ask` in its own build (an unfilled holder refuses the roles write).

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

- The last-administrator rule. `Service.floor` in `modules/user/internal/administration.go` refuses a write that would leave the tenant with nobody who can sign in and administer it. It is called from `SetRoles` and `Deactivate` in `service.go`, and from `RefuseLastAdministrator`, which is `spec.AfterDelete` for the delete route. It takes a per-tenant advisory lock (`administrationLock`) and uses `contracts.CheckedAdministration` and `otherAdministrators`. A refusal rolls back the transaction, and `recordRefusal` publishes `user.administration_refused` in a detached transaction of its own so the attempt is still on the trail (see the section above).
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
