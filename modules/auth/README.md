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
POST   /api/v1/auth/challenge/verify       Public     the second half only — and the cookie
```

The deciding happens in one branch and it happens about the *account*: the first
proof checked out, and if this person holds a factor the answer is
`ErrFactorRequired` — no session, no `auth.logged_in`, and no `auth.login_failed`
either, because the first proof was right and the trail should not record a
success as an attack. `Login` asks it after the password; `Open`, the single
sign-on callback's only caller, asks the same question of the person its provider
just named, because "is what this person proved enough?" has one answer and the
door they happened to knock on does not decide it. A provider that confirmed an
address proved the same thing a password proves, so the federated leg answers
`ErrFactorRequired` as well, as a 401 that says which half was missing and where
the other half is asked for. Both halves of that rule are pinned by
`TestASingleSignOnLegDoesNotWalkPastAPersonsOwnSecondFactor`: it enrols a factor,
shows the password leg opens nothing, shows the provider leg opens nothing, and
last shows a colleague who enrolled nothing is still signed in by that same leg —
so the rule cannot be bought by refusing single sign-on, nor by refusing
everybody. The challenge route
is `httpx.Public()` authorisation on the workspace surface, the shape
`/login` and `/password/reset` already use: the caller has no session by design,
and the answer to a correct code is a session cookie. The anonymous surface sets
no cookie at all — the writer takes every `Set-Cookie` off a response whose
surface is public and the kernel answers such a route with a 500 — so a challenge
answered there could sign nobody in. That is why
`TestASecondFactorSignsThatPersonInOverItsOwnRoute` reads the address off the
composition and sends the request, rather than calling `VerifySecondFactor`: the
leg is the capability, and a service call cannot show that a surface keeps a
cookie away from it.

`Public` on that door means no session is needed to *ask*, not that nothing need
be *proved*. What it authenticates is the half that came before it: `Login` and
`Open`, when they answer `ErrFactorRequired`, mark this account's first factor as
proved — one live row per person in `first_factor_proofs`
(`000033_first_factor_proofs.up.sql`), written outside the refused request because
a 401 rolls its own transaction back, and open for
`contracts.FirstFactorProofWindow` — and `handleVerifyFactor` spends that row (a
`DELETE`, so "once" is the row being gone) *before* it looks at the code. A code
presented with no refused sign-in behind it gets the answer a wrong code gets, at
its cost, having read no factor, spent nothing and published nothing.
That row is the difference between a recovery code that stands in for a second
factor and one that has become a password. Codes are text a person keeps — a
password manager, a file, a printed card in a drawer — so a door that spent one on
presentation would hand out ten sign-ins per enrolment, each usable once, from any
machine, to whoever held the file; and a TOTP is the same door with a 30-second
timer, which is the "read out what your authenticator shows" call a second factor
exists to make worthless. `VerifySecondFactor` still cannot see the first half —
no argument carries it — so it is the pair with `RequireFirstFactorProof`, spent in
the same transaction, that is a sign-in, and the interface says so on both of them
rather than in one comment nobody has to read. The case is
`TestACredentialAnsweredWithoutItsFirstHalfSignsNobodyIn`: one cookie-less POST of
a person's own recovery code, and the ledger read around it — no session row, no
code spent, no `auth.logged_in`, no `auth.recovery_code_used`.

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

* **No tenant-level "my identity provider may stand as the first factor".** The
  brief's "where the tenant allows" is a declaration nobody has written:
  `000030_tenant_oidc` carries an issuer, a client id, a secret reference, a
  redirect path, a registration mode and roles, and `contracts.OIDCProvider`
  mirrors that list and nothing more. So the account decides at both doors, in
  every tenant, and a tenant that wants its provider treated as proof of two
  things has no way to say so. Making that sayable is a column, a field on the
  port, a refusal that reads it and this paragraph rewritten — named here so the
  next brief owns a shape rather than rediscovering one.
* **No `factor_challenges` table.** RFC 6238's step *is* the challenge; the only
  state a table would add is "which steps were spent", which is the one column
  above. Attempts are capped per address instead — see the next bullet.
* **`Limiter.FactorVerify` is not new**: the challenge route reuses `Redeemed`,
  the forgotten-password redemption cap. It is the same decision — how often may
  one address spend a credential this module issued — and a second counter
  meaning the same thing as the first is two knobs for one policy. Six digits
  across three steps is 3×10⁶ guesses per window, which is nothing to a script,
  so this route needs a rate limit and not only a wide space.
* **Passkeys are in.** WebAuthn is `github.com/go-webauthn/webauthn` plus its
  CBOR/COSE tree, priced as a module dependency in its own `build(budget)`
  commit. A passkey is a
  second factor whose proof is a signature over a nonce this server minted, and so
  it is a ceremony rather than a code: `contracts.Passkeys` is beside `Factors`
  because the only enrolment pair `Factors` offers is shaped by a secret the server
  hands out, and nothing in that shape is a parameter of a ceremony. Its own
  `internal/passkeys.go` is the only file in the repository that imports the SDK
  (`contracts/passkeys.go` names no SDK type, so CBOR and COSE never reach a
  handler, an event or the contract) and its own `000035_passkeys.up.sql` the only
  one that adds a challenge table — 000031 declined one for TOTP because RFC 6238
  already is a challenge, and WebAuthn has no such built-in nonce. Nothing there is
  sealed: a public key is public, so a deployment with no `auth.factor_key` enrols
  passkeys and answers both sign-in doors. The two things the library leaves to the
  relying party are decided where they are enforced: the relying party is built per
  request from the host that already chose the tenant (so a passkey minted at one
  tenant's host cannot answer at another's, by construction), and the clone rule —
  a counter going backwards retires the credential for good and publishes
  `auth.factor_suspect` — because the library only flags it. `auth.factor_used`
  names the quieter half: which factor answered. Whether a passkey may be the whole
  sign-in, with no password offered first, is the tenant's own row in
  `passkey_settings`, read per request; `TestTheUsernamelessDoorIsTheTenantsOwnRow`
  is that read. What is *not* here is the administrator's control for that row (the
  product's), any page, and any attestation policy: this module holds no
  manufacturer registry, so which AAGUID made an authenticator stays a specialist
  fact rather than a shared module's table.
* **Bearer tokens are in** (the brief's item 4) — see the next section, and its
  own list of what is still open there.
* **No factor page, and the sign-in page has no second step.** `ui` and the
  admin shell never call `/api/v1/auth/challenge/verify` (only this module's
  routes and its tests name that path), so a person who enrols a factor through
  the JSON routes cannot answer the second half from the reference app's sign-in
  page — the page shows the refusal text and stops. The JSON path does work, and
  is tested at its address: a correct code there is a 200 and a session cookie.
  The page work is the shell's, and until it lands, enrolling a factor from a
  screen that does not know about the challenge route locks a person out of that
  screen.

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

The narrowing is enforced by the kernel rather than by the module that minted
the key, and at both declarations that spend authority: an operation that names a
permission is refused when the list does not carry it, and an operation that names
none *and spends the caller's authority anyway* — every `httpx.SignedIn()` door,
the door about the caller themselves — is refused outright, because with no
permission named the authority such an operation spends is its caller's whole
authority. So the self-service doors are a session's work and never a key's: mint
a key, replace the recovery codes, withdraw a factor, revoke a session, sign out of
every browser. None of them is reachable from a bearer credential whatever its
scopes, which is the half that kept a key scoped to one read from widening itself
back to its holder with two POSTs. `kit/httpx/scoped_credential_test.go` pins both
halves at the kernel, and `review_r4_a_scoped_key_is_held_to_its_scope_test.go`
drives them at the routes.

The rule stops there, at the operations that spend it, and one operation in this
installation names no permission because there is none to name: `GET
/api/v1/app/resources`, mounted by `kit/app`, declares `httpx.AnyCredential()` and
answers a key as readily as a cookie — because it spends nothing on anybody's
credentials. All it does is ask the authorizer, for this caller, which resources
are readable and writable, so the document a narrowed key receives names the
resources its scopes open and no others, where the session that minted it is shown
every resource its roles open.
`TestAScopedKeyReadsTheCatalogAShellIsBuiltFrom` pins both halves: the key reads
the catalog, and sees less of it than the person who made the key.

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
intent. A token request never sets or rotates a session cookie, at any door a
bearer caller reaches — `TestAKeyRequestMintsNoSessionAnywhere` counts the live
cookies at every one of them. `POST /logout` is not such a door: ending a session
is `httpx.SignedIn()` work on the caller's own credentials, so the kernel refuses
it to a bearer caller ahead of the handler, and no cleared cookie is ever on offer
— which is the same verdict as before, stated as it is now reached. A key is
stopped with `POST /tokens/{id}/revoke`, which is a session's work for the same
reason: a credential that could revoke itself would be a credential that could
revoke its holder's other keys.

### Open here, stated rather than approximated

* `GET /api/v1/auth/me` answers a cookie session; a bearer caller is refused it
  by the kernel's rule above, ahead of the handler, and the 403 says which.
  Its body is the person's roles and everything they grant,
  which is wider than a scoped key may act as — reporting that to a narrowed
  caller would be a `/me` that lies about the caller's own authority. A
  "who am I, as this key" answer (the brief's `Identity.Token`) is unbuilt.
* Every route in this module is a session's work, and the one door a bearer key
  reaches in this installation is the kernel's: the resource catalog, which names
  the resources the key's own scopes open. A key therefore finds its work by
  reading `/api/v1/app/resources`, and finds nothing here to manage itself with —
  no self-revocation, no `GET /api/v1/auth/tokens` to see what a holder minted.
* No page: keys are managed through the JSON routes.
* Uses are not audited, and `last_used_at` is the record a person reads; the
  trail keeps the two facts that mean something about a key, that it was made and
  that it was stopped.
