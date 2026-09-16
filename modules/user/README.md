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
