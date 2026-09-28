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
taking away only the grants that seeder wrote — `apps/platformkit repair-roles`
is the door, and `--remove` is the decision. What that flag prints is what its
transaction wrote: two operators running it at once both read the same dead
grant, the second one's write finds the row already cleaned and changes nothing,
and only the run that moved a row says it removed anything. A line saying
`removed` is a claim about a row. A listing stops at the same line, for the same
reason: a role holding a dead grant of somebody else's beside the seeder's is
declined whole — `contracts.CheckedPermissions` refuses the list either write
would produce — so it is named by neither a listing nor a removal, and a run that
prints nothing has not proved there is nothing left. A role's name belonging to the
seeder is not the same as a grant in it being the seeder's: the built-in member
is seeded holding nothing unless the application's initial roles name it, a
customer tenant's administrator is seeded the wildcard and nothing else, and a
permission somebody added through the roles screen stays where it is and goes on
being reported by the hourly sweep — except in the operator's own administrator
row while it still holds the wildcard, the one role the seeder writes named
permissions into, where nothing records who wrote a departed permission and every
dead grant is taken. See `contracts.SeededGrants` for that rule and for what it
cannot tell apart. The repair runs one way: an installation that later *adds* a
module does not gain its operator permissions on roles already seeded, and there
`repair-roles` has nothing to say — the roles screen adds them, the row still
holding the wildcard that admits an operator permission.

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

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt, and the seeded-grant change above composed all of it. **Reused:**
`module.Permission{Key, Operator}` and the catalogue loop `kit/app` already ran
over an application's manifests; `contracts.CheckedPermissions`, which is where
"no module defines it" was already refused, so the seeder gained no rule of its
own; `internal.Undeclared`, so the repair takes back exactly what the hourly
sweep names; `internal.SetRole` for the write, with its tenant lock, its
administration floor and its `auth.role_set` event; and `jobs.PerTenant` for the
walk. **Added:** `contracts.SeededGrants`, because who wrote a grant is a
question no existing unit answered, `Service.SetRoleChanged` — the same write and
the answer it already computed for itself, whether the row changed, which is what
lets a repair say what it did — and `auth.RepairSeededRoles` with the
`repair-roles` subcommand as its only caller. **Made reusable:**
`kit/module.Grants`, the one catalogue of a composition, which was a loop inside
`kit/app`'s manifest check and is now what an application hands the seeder; that
loop is gone and its caller moved to `Grants` rather than a second one being
written. Two other walks of `m.Permissions` remain, and they are different
questions: `validatePermissions` wants a kind per key and `module.Valid` wants an
owner and a duplicate check, neither of which `Grants` produces.
