# Auth module

`modules/auth` is signing in: sessions and passwords, single sign-on through
one OpenID Connect provider, the roles that decide what a caller may do, and
the three opt-in registration modes described in
[ARCHITECTURE.md](../../ARCHITECTURE.md#start-at-the-composition). Routes live
under `/api/v1/auth`, and the doors an anonymous caller may use — the ones
the public surface serves — under `/api/v1/public/auth`; `role:manage` guards
both the two roles routes and the screen the shell serves for them at
`/app/auth/roles`. A write is refused when
it would leave the tenant with no *role* granting `role:manage`; it counts roles
and not the people holding them, so it is a floor and not a guarantee. The user
module has a floor over the people, and the two do not compose: a sequence in
which every write is permitted still reaches a tenant nobody can administer. See
`contracts.CheckedAdministration` for the sequences, for what the fix would look
like, and for which lockouts the control plane's invitation can still repair.
`SeedRoles` runs inside tenant creation, so roles exist before the service does.

A grant is only ever as wide as the composition. `SeedRoles` takes the
application's permission catalogue — `kit/module.Grants` over the modules it
actually composed — and the operator grants the administrator's role is created
holding are the operator permissions of those modules and nothing else; an
initial role naming a permission no composed module defines is refused where it
used to be written. The catalogue used to be a list each application wrote out by
hand, and no application narrowed it when it dropped a module: one served from
2026-09-22 20:11 with an administrator holding `billing:catalog`, a permission
`modules/billing` defines and that installation did not compose, and the only
thing that ever said so was the hourly sweep below — fourteen identical warnings
by the next morning, about a grant no route would ever have accepted. Rows
written by an older seeder are a customer's and are not edited on the way past:
`auth.RepairSeededRoles` lists them per tenant and removes them when asked,
touching only the roles the seeder owns — `apps/platformkit repair-roles` is the
door, and `--remove` is the decision.

Compose it after tenants and notification with `auth.Deps`, naming the user
service, hosts, tenants, the mailer and, when wanted, one of `Registration`,
`ApprovalRegistration` or `EmailRegistration`; the OIDC client secret arrives
through `PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET`. Consumers import
[contracts/](contracts/) — the service, events, permissions and the
[conformance suite](contracts/authtest/) — never `internal/`.
`auth.AdministeringRoles` is the one package-level answer this module owes
another: which of a tenant's roles grant `role:manage`, which is what
`modules/user` needs in order to refuse taking its last administrator away.
[policies/](policies/README.md) shows the optional Topaz policy check.
