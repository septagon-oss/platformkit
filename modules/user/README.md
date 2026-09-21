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
`contracts.CheckedAdministration` is the rule, so the service and the
[fake](contracts/usertest/) refuse identically and the conformance suite holds
both to it at all three doors. It asks two different questions with two
different predicates: `User.Administers` is wide, so removing somebody who has
not accepted their invitation is still a write the floor looks at, and
`User.CanAdminister` is narrow, so somebody who has not accepted theirs cannot
hold the floor up for everybody else — measured, in this repository's own
default configuration, where an invited heir could not in fact take over. What
is refused is the write after which nobody who can sign in would hold such a
role; a grant is never refused, so the refusal always has a way out and the
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

**The shared lock does not make the two floors compose, and this file used to
say it did.** `modules/auth` counts roles; a role nobody holds satisfies it.
Reproduced with both floors in one binary and no concurrency at all —
`TestTheTwoFloorsStillDoNotComposeIntoOneInvariant` runs it and asserts the hole:
create a role granting `role:manage`, give it to nobody, empty the role everybody
holds — two 200s, and the tenant answers 403 to its own roles screen. This rule is the
join **for the writes it sees**, because it reads what the roles grant and
counts the reachable people holding them — it is never consulted about an
auth-module write, so it holds the property for user-module writes and for no
others, which is why the mirror is needed. `modules/auth` needs the mirror of
the dependency this module accepts before a write there is checked against the
same property.

What the rule does **not** cover is written out on `CheckedAdministration`
itself.

`Deps.Administration` answers which of a tenant's roles grant the permission
that can grant every other one back. It is required, and a composition that
omits it panics at `user.Module`: who holds a role is this module's table and
what a role grants is the auth module's, so neither reads the other's rows and
the application joins them — see `apps/platformkit/modules.go`.

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
