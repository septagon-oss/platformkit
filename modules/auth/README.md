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

## Authorization

### Permissions

The manifest declares one permission, `role:manage` (`contracts.PermissionRoleManage` in `modules/auth/contracts/permissions.go`). It guards `auth-role-list` at `GET /api/v1/auth/roles` and `auth-role-set` at `PUT /api/v1/auth/roles/{name}` (`RegisterRoutes` in `modules/auth/internal/handler.go`). It also guards the nav entry `auth/roles` in `modules/auth/module.go`; `modules/admin` serves that screen. There is no `role:read`, and `modules/auth/contracts/permissions.go` says that is deliberate. The other routes name no permission. Login, forgot-password and reset-password are `httpx.Public()`. Logout, identity (me) and change-password are `httpx.SignedIn()` and act only on the caller.

### Object scope

None. The code searched shows no use of `tenancy.Policy` in this module. Role writes happen inside the tenant's own transaction (`SetRole` in `modules/auth/internal/roles.go`). The module also answers other modules' permission questions as the `httpx.Authorizer`, using `contracts.Grants` over the roles table.

### Duties the module enforces itself

`SetRole` refuses a write that removes `role:manage` from the last role that grants it (`contracts.CheckedAdministration` in `modules/auth/contracts/roles.go`). It takes a per-tenant advisory lock first, so two concurrent writes cannot both pass. `contracts.CheckedPermissions` refuses a permission no module defines and refuses an operator permission in a non-operator tenant. `SeedRoles` (`modules/auth/internal/seed.go`) refuses reserved, duplicated or empty default roles and any operator grant in them. The floor counts roles, not people, and the code says it is not a guarantee (see `CheckedAdministration`).

### Public faces

Public routes are registered with `httpx.Public()`: `auth-login`, forgot-password and reset-password in `modules/auth/internal/handler.go`, and, only when the composition opts in, the registration routes (`auth-register` in `registration.go`, `approval_registration.go` and `email_registration.go`, plus `auth-resend-verification` and `auth-verify-email` in `email_registration.go`). Registration acknowledgments are meant to be neutral and not reveal whether an account exists. Public writes are rate-limited through `kit/limit` via `contracts.Limiter` (`NewLimiter(limit.Postgres(...))` in `modules/auth/internal/service.go`): `Check`/`Failed` for login, `MayAsk` for forgot-password and registration, `MayRedeem` for reset and verify, and `VerificationMail` for resend. The exact response fields for each route were not enumerated for this section.

### The operator boundary

The module declares no permission with `Operator: true`, and mounts no `OperatorPermission` route. It handles operator permissions of other modules: `contracts.Grants` never lets the `*` wildcard satisfy an operator grant, and `CheckedPermissions` refuses naming one in a non-operator tenant. `SeedRoles` adds the operator permissions passed by the composition to the `admin` role only in the operator tenant.

### Provisioning

`SeedRoles` creates `admin` (the `*` wildcard, plus the operator permissions in the operator tenant) and an empty `member` role when a tenant is created. The composition calls it from `seedRoles` in `apps/platformkit/modules.go`, passing `tenant:manage` and `billing:catalog` as operator permissions and its two personas as default roles: `coordinator` (`task:read`, `task:update`) and `observer` (`task:read`). `checkPersonas` refuses to compose when a persona grants a permission no composed module declares, or an operator one. `apps/platformkit/persona_test.go` is the persona proof (decision 0011, item 6). It signs in as admin, coordinator, observer and member, drives seven journeys as each, and asserts which are allowed and which are refused with which code. Roles are then changed through `PUT /api/v1/auth/roles/{name}` or the roles screen, by a holder of `role:manage`. A role in a client's `client.yaml` is not shown by the code read for this section.
