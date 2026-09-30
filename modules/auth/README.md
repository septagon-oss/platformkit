# Auth module

`modules/auth` is signing in: sessions and passwords, single sign-on through
each tenant's own OpenID Connect provider (the installation may name one of its
own, and a tenant's row names another), the roles that decide what a caller may
do, and the three opt-in registration modes described in
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
`ApprovalRegistration` or `EmailRegistration`; the installation's own OIDC
client secret arrives through `PLATFORMKIT_AUTH_OIDC_CLIENT_SECRET`, and a
tenant that names its own provider through `Deps.OIDCProviders` and
`Deps.Secrets`, which resolve the issuer and the secret reference that row
holds — the reference is an environment variable's name and the secret is never
stored. Consumers import
[contracts/](contracts/) — the service, events, permissions and the
[conformance suite](contracts/authtest/) — never `internal/`.
`auth.AdministeringRoles` is the one package-level answer this module owes
another: which of a tenant's roles grant `role:manage`, which is what
`modules/user` needs in order to refuse taking its last administrator away.
[policies/](policies/README.md) shows the optional Topaz policy check.

## Authorization

### Permissions

The manifest declares one permission, `role:manage` (`contracts.PermissionRoleManage` in `modules/auth/contracts/permissions.go`). It guards `auth-role-list` at `GET /api/v1/auth/roles` and `auth-role-set` at `PUT /api/v1/auth/roles/{name}` (`RegisterRoutes` in `modules/auth/internal/handler.go`). It also guards the nav entry `auth/roles` in `modules/auth/module.go`; `modules/admin` serves that screen. There is no `role:read`, and `modules/auth/contracts/permissions.go` says that is deliberate. The other routes name no permission. Login, forgot-password and reset-password are `httpx.Public()`. Logout, identity (me), change-password and the three session routes (`auth-session-list`, `auth-session-revoke`, `auth-session-revoke-all`) are `httpx.SignedIn()` and act only on the caller: the list names the machines this person is signed in on without ever carrying a session id, and the two revocations end one of them or all of them, including the request that asked. `modules/admin` serves that list as a page at `/app/auth/sessions`. It has no nav entry: `kit/module.Validate` refuses an entry that names no permission ("a link everyone sees is still a decision"), and the two permissions this module could name for it would both be wrong — `role:manage` hides the screen from the members it is for, and inventing a `session:read` for one's own sessions would be a permission no role can be refused. The product that owns the navigation names the entry beside the permission it seeds.

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

## Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** `contracts.Session` and its `id_hash` key, `SessionRef`,
`httpx.Register` with `httpx.SignedIn`, `crud.ErrNotFound`, the `user_agent`
and `ip` columns `000008` already keeps, `events.Declare`, and `modules/audit`,
which records whatever the outbox carries; a per-tenant provider is
`SetLocale`'s shape — the fact on the tenant's row, the mechanism in the
module, the resolution inside the transaction the `Host` header already
resolved. **Added:** `SessionListing` (a session row is a credential and a list
a person reads is not, so `Session` could not be the response type without
putting a live cookie in it), the ref-keyed revocation commands and
`auth.session_revoked`, the row lock that makes two tabs revoke one session
once, the six `oidc_*` columns on `tenants` with the two CHECKs that make half
a provider unstatable, and `auth`'s `OIDCProviders`/`Secrets`/`Provisioner`
ports — none of which anything existing carried, because `RevokeSessions` takes
a user id and no list can name a session by one. **Made reusable:**
`contracts.SessionRefOf`, for any later surface that names a session by ref; a
provider cache keyed by issuer rather than by tenant, so two tenants sharing a
door discover it once; and the discovery-port shape itself — a module that asks
its composition per request instead of holding a client it resolved at boot.

## A second factor

A password proves something was typed. A second factor proves something was
carried, and until `000031_auth_factors.up.sql` the first proof was the only
proof: a leaked password *was* the account, with every session and revocation in
this schema guarded by it.

`contracts.Factors` is the capability, and it is its own interface rather than
more methods on `Service` for the reason the mailer and the limiter are their own
ports: a composition that wants passwords only composes none of this. The routes
mount only for a deployment that set `auth.factor_key`, the key that seals an
enrolled secret at rest — with no key there is no door that can only answer
"unavailable", and signing in stays exactly as it was.

```
POST   /api/v1/auth/factors/totp/begin     SignedIn   the secret, once, base32 and as an otpauth URI
POST   /api/v1/auth/factors/totp/finish    SignedIn   enrols it, and hands out the recovery codes
GET    /api/v1/auth/factors                SignedIn   what this account proves beside its password
DELETE /api/v1/auth/factors/{id}           SignedIn   withdraw one; the last is refused
POST   /api/v1/auth/factors/recovery/rotate SignedIn  retire every unused code, issue a fresh set
POST   /api/v1/auth/challenge/verify       Public     the second half of a sign-in, and the cookie
```

`Login` does the deciding in one branch: the password checked out, and if this
person holds a factor the answer is `ErrFactorRequired` — no session, no
`auth.logged_in`, and no `auth.login_failed` either, because the password was
right and the trail should not record a success as an attack. The challenge route
is `httpx.Public()` authorisation on the workspace surface, the shape
`/login` and `/password/reset` already use: the caller has no session by design,
and the answer to a correct code is a session cookie. The anonymous surface sets
no cookie, so a challenge answered there could sign nobody in.

TOTP is RFC 6238 in `internal/totp.go` over `crypto/hmac` — the register's own
rule for this capability (`T-0013`: nothing beyond the standard library and
`x/crypto`), and the ~30 lines are checkable against the RFC in one sitting; its
Appendix B vectors are in `totp_test.go`, checked against an independent
implementation rather than against this one. Recovery codes are 128 bits from
`crypto/rand`, hex, hashed with SHA-256 for the reason `sessions.id_hash` gives:
the input is not something a person chose, so a slow hash buys nothing. A
correct code is a nonce: the factor row remembers the highest step it accepted
(`last_step`) and the spend is `UPDATE … WHERE last_step < ?`, which is the
refusal and the record in one statement.

### Deliberately not here

* **No `factor_challenges` table.** RFC 6238's step *is* the challenge; the only
  state a table would add is "which steps were spent", which is the one column
  above. Attempts are capped per address instead — see the next bullet.
* **`Limiter.FactorVerify` is not new**: the challenge route reuses `Redeemed`,
  the forgotten-password redemption cap. It is the same decision — how often may
  one address spend a credential this module issued — and a second counter
  meaning the same thing as the first is two knobs for one policy. Six digits
  across three steps is 3×10⁶ guesses per window, which is nothing to a script,
  so this route needs a rate limit and not only a wide space.
* **No passkeys.** WebAuthn is `github.com/go-webauthn/webauthn` plus its
  CBOR/COSE tree — a new module dependency, priced in its own `build(budget)`
  commit, and a `webauthn.Config` built per request from the resolved host. That
  is a round of its own and this one did not take it; the `Kind` field on
  `Factor` and on the two factor events is where it lands when it does, and
  nothing above has to change for it.
* **No bearer tokens** (the brief's item 4): no token table, nothing issued as
  `Authorization: Bearer`, and no `Principal.Permissions`. Still open.
* **No factor page.** The JSON routes are the surface; a screen would be the
  shell's, and this module's own nav entry is the sessions one.

## Bearer tokens for a person's own integrations

A session cookie proves a browser. Nothing proved a script until
`000032_api_tokens.up.sql`: the only credential this platform accepted was a
cookie, so a mobile shell or a deploy bot had to hold a person's password and
impersonate their browser.

```
POST /api/v1/auth/tokens              SignedIn   mint a key; the token is in this response and nowhere else
GET  /api/v1/auth/tokens              SignedIn   the person's own keys: name, scopes, expiry, last use, revoked
POST /api/v1/auth/tokens/{id}/revoke  SignedIn   stop one key, leaving the person's own sessions alone
```

A key is a **narrowed** credential, never an escalated one. Its scopes are
checked twice before a row exists — against every permission the application
declares, and against what the holder's own roles already grant — and a scope
that is the wildcard is refused outright. The authorizer is then held to the
credential: `tenancy.Principal.Permissions` is a ceiling, and when it is not nil
the roles are not consulted, because resolving them would answer a question about
the holder rather than about the caller. That is the whole of why a scoped key
carries no roles. `Allowed` follows the same rule the declaration does, from the
same package, with the same `Grants` predicate — which is also why an
administrator holding the wildcard can mint a narrow key at all.

The intersection is recomputed from the roles on every request, so standing
somebody down from a role narrows the keys they minted in the same transaction
that did it; `TestStandingARoleDownNarrowsTheKeysThatPersonMinted` is that claim.
`last_used_at` is throttled by `APITokenTouch` and **never** extends
`expires_at`: a token that renews itself on use is an expiry in name only, which
is the argument `SessionMaxLifetime` already makes about a session. `created_by`
carries no cascade — deleting a person must not silently delete a key another
integration depends on.

The kernel's part is three lines and a field: `httpx.BearerOf` (RFC 7235's
scheme, case-insensitive, and a request that names the scheme twice with two
different credentials is refused rather than resolved), `credentialed` accepting
a bearer, and an ambiguity check that answers such a request as **anonymous** —
a caller who presented a cookie *and* a key is not more signed in, and which one
they meant is not the kernel's to guess. `csrf.go` is unchanged and that is the
point: its gate is the session cookie, and a bearer carries its own proof of
intent. No cookie is ever set, rotated or cleared by a token request.

### Open here, stated rather than approximated

* `GET /api/v1/auth/me` answers a cookie session and refuses a bearer caller with
  a 403 that says so. Its body is the person's roles and everything they grant,
  which is wider than a scoped key may act as — reporting that to a narrowed
  caller would be a `/me` that lies about the caller's own authority. A
  "who am I, as this key" answer (the brief's `Identity.Token`) is unbuilt.
* No page: keys are managed through the JSON routes.
* Uses are not audited, and `last_used_at` is the record a person reads; the
  trail keeps the two facts that mean something about a key, that it was made and
  that it was stopped.
