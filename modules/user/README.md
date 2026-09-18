# User module

`modules/user` is the people in a tenant: one entity, a `rest.Spec` at
`/api/v1/user/users` guarded by `user:read` and `user:manage`, invitations at
`/api/v1/user/invitations`, explicit lifecycle commands, and pending
registrations for compositions that want operator approval (`user:approve`
guards the approve-registration command). A user belongs to a tenant by
carrying its id, which row-level security matches on, so the module takes no
dependencies: `user.Deps{}`.

`Provision` is what `platformkit bootstrap` calls to create the first
administrator with a password; the argon2id parameters are constants here.
Consumers import [contracts/](contracts/) — users, registration, password
rules, events, permissions and the [fake](contracts/usertest/) — never
`internal/`. The admin entry is `/admin/user/users`.

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
