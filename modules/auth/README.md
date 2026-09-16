# Auth module

`modules/auth` is signing in: sessions and passwords, single sign-on through
one OpenID Connect provider, the roles that decide what a caller may do, and
the three opt-in registration modes described in
[ARCHITECTURE.md](../../ARCHITECTURE.md#start-at-the-composition). Routes live
under `/api/v1/auth`; `role:manage` guards the roles screen at
`/admin/auth/roles`. `SeedRoles` runs inside tenant creation, so roles exist
before the service does.

Compose it after tenants and notification with `auth.Deps`, naming the user
service, hosts, tenants, the mailer and, when wanted, one of `Registration`,
`ApprovalRegistration` or `EmailRegistration`; the OIDC client secret arrives
through `PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET`. Consumers import
[contracts/](contracts/) — the service, events, permissions and the
[conformance suite](contracts/authtest/) — never `internal/`.
[policies/](policies/README.md) shows the optional Topaz policy check.
