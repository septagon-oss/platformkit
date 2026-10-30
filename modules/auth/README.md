# Auth module

## Composition

Compose `auth.Grants` before `user.Module` and `auth.Module` in the application's
`Use` list. The resolver orders the independent `authgrants` provider before both
consumers. It publishes `contracts.GrantAuthorizer` and `contracts.PermissionReader`,
`usercontracts.Granting` and `usercontracts.Administration`; it opens no resources
and owns no migrations. The role tables remain in Auth's migration set.

**Reused** — `kit/tenancy`, the existing role queries, `usercontracts.CheckedAdministration`,
request transactions and pkit's typed declarations carry the policy and composition.
**Added** — [the grant decision](contracts/grants.go) separates permission decisions
from session construction, which previously prevented User from constructing independently.
**Made reusable** — provider-owned [capabilities](contracts/capabilities.go), a shared
fake/SQL grant suite and `RepairSeededRolesWith` let consumers request only their operations.

`New` accepts `Authorization` and `Permissions` together; omitting both creates the
same independent policy locally for compatible callers, while a partial pair panics.
The policy reads the explicit tenant transaction, honors a non-nil credential ceiling,
and requires explicit permissions for operator grants. `Service` and `Auth` remain
deprecated aggregates. Registration providers request only their chosen User capability.

`modules/auth` is signing in: sessions and passwords, single sign-on through
each tenant's own OpenID Connect or SAML provider (the installation may name one
of its own over OIDC, and a tenant's row names another of either kind), the roles
that decide what a caller may do, and the three opt-in registration modes
described in
[ARCHITECTURE.md](../../ARCHITECTURE.md#start-at-the-composition). Routes live
under `/api/v1/auth`, and the doors an anonymous caller may use — the ones
the public surface serves — under `/api/v1/public/auth`; `role:manage` guards
both the two roles routes and the screen the shell serves for them at
`/app/auth/roles`. A write is refused when
it would leave the tenant with nobody who can sign in and administer it: the
rule is `user/contracts.CheckedAdministration`, and
`contracts.CheckedAdministration` is this module's door onto it, asking
`contracts.Users.Holders` for the people behind the names. It is one rule with
the user module's floor, not two halves of one.
`SeedRoles` runs inside tenant creation, so roles exist before the service does.

A grant is only ever as wide as the composition. `SeedRoles` takes the
application's permission catalogue — `kit/module.Grants` over the modules it
actually composed — and the operator grants the administrator's role is created
holding are the operator permissions of those modules and nothing else; an
initial role naming a permission no composed module defines is refused where it
used to be written. The refusal arrives at the first tenant provisioned, inside
that tenant's own creation transaction — it writes no tenant row and emits no
event — and not at process start: `kit/app` refuses boot for an *operation*
guarded by an undefined permission and has no line for an initial role, which is
a value a composition closes over rather than one the kernel can see into. The catalogue used to be a list each application wrote out by
hand, and no application narrowed it when it dropped a module: one served from
2026-09-22 20:11 with an administrator holding `billing:catalog`, a permission
`modules/billing` defines and that installation did not compose, and the only
thing that ever said so was the hourly sweep below — fourteen identical warnings
by the next morning, about a grant no route would ever have accepted. Rows
written by an older seeder are a customer's and are not edited on the way past:
`auth.RepairSeededRoles` lists them per tenant and removes them when asked,
taking away only the grants that seeder wrote — `apps/platformkit repair-roles`
is the door, and `--remove` is the decision. What it subtracts from is the roles
as they stand *under* the tenant's role lock, which it takes and re-reads before
it derives anything: subtract from the row as it looked when the walk arrived and
the command revokes a grant somebody had just made and puts back one they had
just taken away, and only the second of those is invisible in what it prints.
What that flag prints is what its transaction wrote: two operators running it at
once both find the same dead grant, the second finds the row already cleaned when
it takes the lock and writes nothing at all, and only the run that moved a row
says it removed anything. A line saying `removed` is a claim about a row. A
listing stops at the same line, for the same reason: a role holding a dead grant of somebody else's beside the seeder's is
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

## Composition

**Reused** — the module's own `503 "account email delivery is unavailable"` refusal and its
no-write path, which is the whole of what every door now answers with; `contracts.Mailer`,
`contracts.Hosts` and `contracts.MailLedger` as the capabilities they are; and the
`pkit.Needs` / `pkit.Get` edge the module already declared for the sender.

**Added** — `contracts.MailDelivery` in `Deps` and one predicate,
`internal.Delivery.deliversAccountMail()`, because a composition holds a `Mailer` whether or
not the deployment named a mail host, so the nil check these doors read could no longer be
true and an installation that can never mail was answering 202, committing an account and an
address-bearing event. The predicate is fail-closed: a port nobody wired reads as "nothing is
delivered", the same answer as a deployment with no mail host.

**Made reusable** — `authtest.Delivers` and `authtest.Undelivers`, `Mailbox.Undeliverable`
and `authtest.NoMailDetail`, and one conformance case that the fake and the SQL-backed
service must answer alike: the next fixture says which installation it stands in, and the
promise a mail-less installation cannot keep is refused by both implementations or by neither.

Which doors ask it: sign-up at `POST /api/v1/public/auth/register` (both the email-confirmation
mode and the invitation mode), `POST /api/v1/public/auth/resend-verification`, and the
forgotten-password request. The last is refused by `Service.Forget` itself, before it
publishes: the acknowledgment that route answers with promises a mail, and an outbox row no
installation can discharge is a promise kept in a table and broken in the world. The refusal
rolls the caller's transaction back and publishes nothing — the event lives in that
transaction, so a refused request leaves no token and no ledger row either. It is asked inside
the command rather than on the route because the route is mounted against the frozen
`contracts.Service`, which every installation's own wiring already answers.
What is deliberately left alone: approval sign-up, which commits a pending account and promises
an operator's decision rather than a mail. The password travels to the user owner inside that
request, so the account it commits already holds the credential its person chose, and
`Service.Offer` sends a set-password link only to a person in `user.invited` — nothing in this
module ever mails the approved one, so the door is asked nothing it cannot keep. Refusing it
would turn down a promise the installation can honour; the same installation still refuses the
three emailed doors beside it, which is the boundary a case holds. Also left alone: the redeem
doors, which consume a credential somebody was already sent, and the mails raised inside a
transaction, which still reach the carrier and are recorded `suppressed` where nothing delivers.

## Authorization

### Permissions

The manifest declares two permissions. `role:manage` (`contracts.PermissionRoleManage` in `modules/auth/contracts/permissions.go`) guards `auth-role-list` at `GET /api/v1/auth/roles` and `auth-role-set` at `PUT /api/v1/auth/roles/{name}` (`RegisterRoutes` in `modules/auth/internal/handler.go`). It also guards the nav entry `auth/roles` in `modules/auth/module.go`; `modules/admin` serves that screen. There is no `role:read`, and `modules/auth/contracts/permissions.go` says that is deliberate.

`passkey:signin` (`contracts.PermissionPasskeySignIn`) guards one operation, `auth-passkey-sign-in-set` at `POST /api/v1/auth/settings/passkey-sign-in`, which is the tenant's own answer to whether a passkey may be the whole sign-in (`SetPasskeySignIn` in `modules/auth/internal/passkeys.go`, `passkey_routes.go`). It is its own key rather than a borrowing of `role:manage` for the reason `modules/auth/module.go` gives about the session list — borrowing that key for a second purpose "would be the wrong decision twice over" — and it is an ordinary permission rather than an operator one because the row it writes belongs to the tenant whose people it changes, which is the argument `migrations/000035_passkeys.up.sql` makes for the table itself. Nothing in this module seeds it: a tenant's `admin` role holds it through the `*` wildcard `SeedRoles` gives it, and the empty `member` role does not, which is the refusal `modules/auth/internal/the_usernameless_door_opens_for_the_permission_it_changes_test.go` pins. No nav entry names it, for the same reason the session list has none.

The other routes name no permission. Login, forgot-password and reset-password are `httpx.Public()`. Logout, identity (me), change-password and the three session routes (`auth-session-list`, `auth-session-revoke`, `auth-session-revoke-all`) are `httpx.SignedIn()` and act only on the caller: the list names the machines this person is signed in on without ever carrying a session id, and the two revocations end one of them or all of them, including the request that asked. `modules/admin` serves that list as a page at `/app/auth/sessions`. It has no nav entry: `kit/module.Validate` refuses an entry that names no permission ("a link everyone sees is still a decision"), and the two permissions this module could name for it would both be wrong — `role:manage` hides the screen from the members it is for, and inventing a `session:read` for one's own sessions would be a permission no role can be refused. The product that owns the navigation names the entry beside the permission it seeds. The six passkey ceremony routes likewise name none: the enrolment pair is `httpx.SignedIn()` and about the caller's own device, and the four sign-in legs are `httpx.Public()` because the caller has no session by design — what they authenticate is a window the `/login` refusal minted, or a signature over a nonce this server wrote for this tenant's host, and `passkey_routes.go` says why that is not the same thing as nobody.

### Object scope

None. The code searched shows no use of `tenancy.Policy` in this module. Role writes happen inside the tenant's own transaction (`SetRole` in `modules/auth/internal/roles.go`). The module also answers other modules' permission questions as the `httpx.Authorizer`, using `contracts.Grants` over the roles table.

### Duties the module enforces itself

`SetRole` refuses a write that removes `role:manage` from the last role that grants it (`contracts.CheckedAdministration` in `modules/auth/contracts/roles.go`) and publishes `auth.administration_refused` for the attempt, in a detached transaction of its own, because the write it describes was rolled back and the request's transaction with it — the same shape `recordFailure` uses for a failed login, and logged rather than returned if the record cannot be written. It takes a per-tenant advisory lock first, so two concurrent writes cannot both pass. `contracts.CheckedPermissions` refuses a permission no module defines and refuses an operator permission in a non-operator tenant. `SeedRoles` (`modules/auth/internal/seed.go`) refuses reserved, duplicated or empty default roles and any operator grant in them. The rule counts the people who would still hold a role that grants it, so a role held by nobody answers for nothing (see `CheckedAdministration`).

### Public faces

Public faces are registered with `httpx.Public()`: `auth-login`, forgot-password and reset-password in `modules/auth/internal/handler.go`, and, only when the composition opts in, the registration routes (`auth-register` in `registration.go`, `approval_registration.go` and `email_registration.go`, plus `auth-resend-verification`, `auth-verify-email` and `auth-mail-delivery` in `email_registration.go`). Registration acknowledgments are meant to be neutral and not reveal whether an account exists. `auth-mail-delivery` is the one read of the notification module's direct-mail record: keyed by the caller's own `X-Request-ID`, it answers 200 with one field and one of **two** words — `failed` when a transport refused the mail, `pending` for everything else, including a record that says the mail went out. The record it reads is the caller's own and only that one, and only of the two kinds the sign-up and resend flow produce, because every branch of those two routes hands exactly one message to the transport — a link or the message that says no link was sent (`internal/no_link.go`) — so no caller is answered out of somebody else's row and no caller is answered from the absence in front of it. The forgotten-password route is not among those kinds: it mails only the addresses where it found an account, so its refusals are sayable to nobody (see [The record a mail leaves](#the-record-a-mail-leaves)). Review 3 removed the second read that used to stand behind this one — the tenant's newest outcome, for a caller whose own call had left no record — because a stranger could plant the refusal it was answered with. It names no recipient, no kind and no reason, a call that left no record is answered exactly as one from another tenant is, and a call whose mail went is answered exactly as one that mailed nothing, because `sent` about an address is a statement about who has an account here (`contracts.MailReport` is that refusal, and `TestTheMailDeliveryDoorDoesNotTellAKnownAddressFromAnUnknownOne`, `TestFailedMailDoesNotDiscloseWhetherAnAccountExists`, `TestFailedRegistrationMailDoesNotDiscloseAnExistingAccount` and `TestFailedVerificationResendDoesNotDiscloseAnActiveAccount` are its proofs). Public writes are rate-limited through `kit/limit` via `contracts.Limiter` (`NewLimiter(limit.Postgres(...))` in `modules/auth/internal/service.go`): `Check`/`Failed` for login, `MayAsk` for forgot-password and registration, `MayRedeem` for reset and verify, `AskedAboutMail` for the delivery read — its own budget rather than the redemption one, because the shell polls the read while a person waits for a link and must not spend the link's use on waiting — and `VerificationMail` for resend. The exact response fields for each route were not enumerated for this section.

### The operator boundary

The module declares no permission with `Operator: true`, and mounts no `OperatorPermission` route. It handles operator permissions of other modules: `contracts.Grants` never lets the `*` wildcard satisfy an operator grant, and `CheckedPermissions` refuses naming one in a non-operator tenant. `SeedRoles` adds the operator permissions of the composition's own catalogue to the `admin` role only in the operator tenant — the catalogue is `kit/module.Grants` over the modules the application composed, not a list one of them writes out.

### Provisioning

`SeedRoles` creates `admin` (the `*` wildcard, plus the operator permissions in the operator tenant) and an empty `member` role when a tenant is created. The composition calls it from `seedRoles` in `apps/platformkit/roles.go`, passing the catalogue the resolved sentence declares (`composition.catalogue`, which is `module.Grants(c.modules)`) as the permissions and its two initial roles as the defaults: `coordinator` (`task:read`, `task:update`) and `observer` (`task:read`). So `tenant:manage` and `billing:catalog` are in the operator tenant's `admin` role because this application composes `modules/tenant` and `modules/billing`, and an application that composes neither cannot seed them. `checkPersonas` refuses to compose when a persona grants a permission no composed module declares, or an operator one. `apps/platformkit/persona_test.go` is the persona proof (decision 0011, item 6). It signs in as admin, coordinator, observer and member, drives seven journeys as each, and asserts which are allowed and which are refused with which code. Roles are then changed through `PUT /api/v1/auth/roles/{name}` or the roles screen, by a holder of `role:manage`. A role in a client's `client.yaml` is not shown by the code read for this section.

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

For the mail record: **Reused** — `internal.record`'s raw-SQL write shape, and
`verificationLock`'s advisory lock: `offer` takes the same
`pg_advisory_xact_lock` over `auth/password/<tenant>/<user>` before `recent()`, so
two reset requests for one person mail one link. The RLS pattern of `000027`,
`served.go`'s "a fact from the event, needed two frames later" slot (now
`WithOrigin`/`originOf`, carrying the delivered event's `RequestID` and
`Traceparent` down to the row that names the call), `contracts.EmailKey` for the
recipient, and `modules/audit`'s `SubscribeAll`, which puts `auth.mail_failed` on
the trail with nothing registered anywhere to make that true. **Added** —
`contracts.MailLedger`, which nothing carried because the two direct sends wrote
nothing down at all, the `Mails` port on `auth.Deps`, the `auth.mail_failed` event,
`contracts.Limiter.AskedAboutMail`, and the door. **Made reusable:**
`contracts.RedactMailReason` and `contracts.MailReport` live in the notification
module's `contracts/` rather than here, so a second module that mails outside a
notice records it and answers about it the same way instead of inventing a scrubber
of its own.

For a key retiring itself: **Reused** — `contracts.Tokens` (extended, not
paralleled), `RevokeToken`'s two-column read and guarded `UPDATE`,
`EventAPITokenRevoked` and its payload unchanged, the bearer resolution in
`bearerCaller`, `tenancy.Principal`, and the catalogue's `AddCommand`.
**Added** — `Principal.TokenID`, because nothing on a principal said which key was
acting; `RevokeOwnToken` and `ErrCallerWithoutKey`, because no command could take
"the caller's own key" without taking an id; the `auth-token-revoke-self` door;
the kernel's `httpx.BearerKey()`, because no declaration admitted a key and
refused a session; a catalogue entry for a resource read by nobody, because a
command could only be found on a readable resource; and the fake's token half
with `authtest.RunTokens`, because no token conformance suite existed. **Made reusable:** `authtest.RunTokens` and its `TokenFixture`, for
any second implementation of `contracts.Tokens`, and `Principal.TokenID` for any
later command that must act on the credential rather than on its holder.

For the seeded grant: **Reused:**
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

For the pages a mailed link opens: **Reused** — `internal.Delivery` and the
tenant's host of record every link is built on, unchanged; `ResetPath` and
`VerifyEmailPath` as the defaults an empty page keeps; `httpx.LocalPath` for "a
path on this site"; the `pkit.Optional` and `pkit.Get` pair the SAML providers
already use; and the signup doors' one-line module shape for `MailedLinksOpen`.
**Added** — `LinkPages` and `Deps.LinkPages`, because no field let a composition
name a page other than the admin shell's, and `httpx.UnderAPI`, so the API root
keeps its one spelling in `kit/httpx`. **Made reusable:** `httpx.UnderAPI`, for
the next module that must refuse a named page under the API, and
`MailedLinksOpen`'s shape — a value the composition supplies, refused at the plan
rather than at its first use.

For the account-request budget: **Reused** — `contracts.Limiter`, `NewLimiter`
and `reserve` over `kit/limit.Reserve`, which already took a quota and a window
per call; `Decision.Policy`, `asked` and `httpx.SayCounted`, so the 429 and its
`RateLimit` headers state the configured figure with no change of their own;
`config.Auth` with the pointer-for-omitted shape of `Telemetry.SampleRatio`;
`OIDCFromConfig`'s converter shape and `wire`'s one read of the block; and
`internal.Delivery` to carry the budget to `NewService`. **Added** —
`contracts.AccountRequestBudget` with `Check` and `NewBudgetedLimiter`, because
`Policy` carries a name a configuration must not set and the quota and window
were package constants nothing could move; `Deps.AccountRequests`,
`config.AccountRequests` with its loader refusal of an explicit zero;
`limit.MaxWindow`, the export of the day the sweep keeps, and `limit.MaxQuota`,
the largest limit the counter's integer can refuse past once `Reserve` stops it
at its maximum, so each ceiling is stated once. **Made reusable:**
`limit.MaxWindow` and `limit.MaxQuota`, for the next configured budget that must
stay inside what the counter keeps, and `NewBudgetedLimiter`, which takes the
budget a second composition names. Inside the package, `reserve`
takes its window as an argument, so a second cap can be configured without
touching the lockout's.

## The pages a mailed link opens

The set-password link (an invitation or a reset) and the sign-up confirmation
link open the admin shell's workspace screens, `/app/auth/reset` and
`/app/auth/verify-email`, on the host the tenant is served at. A composition whose
people never see the admin shell names its own pages in `Deps.LinkPages`:
`SetPassword` and `ConfirmEmail`, each a path on the tenant's host. The reset
notice leads to the same set-password page without the token, which stays in the
mail and nowhere else. The page reads `token` from its query and calls the door
that exists for it — `POST /api/v1/auth/password/reset` or
`POST /api/v1/public/auth/verify-email` — because this module serves no page at
either address.

A pkit composition names them with one module beside `auth.Module`:

```go
pkit.NewApp("shop").Use(
	auth.Module,
	auth.MailedLinksOpen(auth.LinkPages{
		SetPassword:  "/account/password",
		ConfirmEmail: "/account/confirm",
	}),
	// the rest of the composition
)
```

Either may be named alone; the empty one keeps its workspace screen, and naming
neither mails exactly the links mailed before the field existed. A page that is
not an absolute path on this site — relative, on another host, with a scheme or a
backslash, or with a query or fragment the appended `?token=` would collide with
— or that opens under the API root `/api/v1` once a browser has resolved its dot
segments, percent-encoded ones included, is refused when the composition is
built: `MailedLinksOpen` fails the plan, and `New` panics for a composition that
fills `Deps` itself (`TestNamedPagesAreWhereTheMailedLinksAndTheResetNoticeLead`,
`TestNewRefusesALinkPageThatIsNotAnAbsolutePath`,
`TestNewRefusesALinkPageUnderTheAPIRoot`,
`TestNewRefusesAnEncodedPageUnderTheAPIRoot`,
`TestComposingARefusedLinkPageFailsThePlan`,
`TestCompositionRefusesAnEncodedPageUnderTheAPIRoot`).

## The record that a mail was decided

A credential link is committed before the message carrying it is handed over, so a
process can stop between the two and leave a live token nobody was ever sent. The
ledger above cannot see that: `direct_mail_deliveries` is written when a transport
answers, and this is the window before any transport answered.

`auth_mail_dispatch` (migration 000056) closes it as evidence rather than as a queue.
One row is written per decided message, in the minting transaction, naming the
credential only by its hash, with the kind, the address and `state = unsent`; the
hand-off marks the same row `sent` or `withdrawn` in its own transaction, beside the
marks of the send. A row still `unsent` after the grace — four times the ceiling on one
hand-off — becomes `unknown` in the hourly sweep, and nothing may ever make it `sent`,
because no memory holds the plaintext any more and no carrier ever held it. A hand-off
that comes back after the sweep settled its row keeps every one of its marks back, not
only this column: `sent_at`, the notice and the ledger row are marks of that same
message, and a row saying the send is unknowable is the record they would contradict.
The link such a hand-off mailed still answers until the next offer replaces it, so the
person asking again is the recovery there too. A settled row is forgotten after ninety
days; an unsettled one outlives every sweep, since it is the crash's evidence and
deleting it would hide the loss.

So a never-sent link is a queryable state, and the recovery is the person asking
again: both resend cooldowns count `sent_at`, which a lost send never wrote. This
table makes no send survivable and no send exactly-once — the row says at most what
the transport proved, and a hand-off that died across the socket leaves `unsent`
beside a mail that did arrive. It has no read of its own: answered by request id or
address, "is their mail still unsent" is the enumeration oracle the delivery door
exists to refuse, so an operator reads the table in SQL. A rotation of a link the
person already holds is the one decided send with no row — its mail leaves before its
credential is written, so only the transaction holding the transport's answer can
write its evidence.

Three files make those claims falsifiable: `a_credential_mail_is_recorded_before_it_leaves_test.go`
for the write, the rollback, the refused send and the filing carrier,
`no_dispatch_row_names_a_plaintext_credential_test.go` for what a row may hold and which
tenant may read it, and `a_lost_send_stays_unsent_then_unknown_test.go` for the grace, the
sweep and the hand-off that came back to a row the sweep had already settled.

## The record a mail leaves

Two mails go out of this module with no notice behind them — a set-password
link (`contracts.MailSetPassword`, from a reset request and from an invitation)
and a verification link (`contracts.MailVerification`, from sign-up and from
resend). They are recorded in the notification module's `direct_mail_deliveries`
through `contracts.MailLedger`, in the transaction of the send itself: one row
per attempt saying who it went to, which kind it was, whether the transport took
it (`sent`), refused it (`failed`, with the transport's words redacted by
`contracts.RedactMailReason`) or had nothing to send it through (`suppressed`).
A refusal is acknowledged rather than retried, which is what lets its row commit,
and it leaves no credential behind: `offer` restores the token row it found and
raises no notice, because the in-app copy says "the link is in the email this
raised" and that sentence is owed only by a record that says `sent`
(`TestARefusedSetPasswordMailLeavesNoNoticeThatSaysItWasSent`). The failure is on
the trail as `auth.mail_failed`, in the same transaction as the row.

Nothing in a record is a secret: no subject, body, link, token or hash —
`TestMailedLinkIsRecordedAndCarriesNoCredential` and
`TestRefusedMailIsRecordedWithoutItsCredential` scan every base table in the
schema, enumerated from `information_schema` rather than named, for the token and
its hash.

The one public read is `POST /api/v1/public/auth/mail-delivery` — see
[Public faces](#public-faces) for its two words and the reason `sent` is not one
of them. `ui/assets/js/session.js` asks it after a register or
resend-verification form is accepted, with the `X-Request-ID` that call was
answered with, and replaces the neutral acknowledgment with "mail could not be
sent just now: the mail server refused it" only on `failed`; the neutral sentence
itself never claims a mail left. It does not ask after the forgotten-password
form, which has no answer to receive (below). The door's address is the module's
mount plus `/mail-delivery` — `kit/httpx/surfaces.go`'s shape, not the last
segment of the route that answered, which is how a `/password/forgot` form came
to ask a path nobody mounts and to leave a refusal unsaid.

### Limits

* **The door answers two words, and `sent` is not one of them.** A route that
  mails only addresses where somebody has an account cannot report whether a mail
  left without reporting who has an account, so a person who asks is told that
  nothing failed rather than that something succeeded. The brief's "'we sent you a
  link' is said only when a record says `sent`" is honoured by the shell never
  claiming it at all; showing `sent` to anybody is a product decision.
* **Which refusals are sayable is decided by the flow, not by the door.** The door
  reads its answer among `auth.verification` and `auth.verification_no_link`
  (`internal/mailDeliveryKinds`), and every branch of the sign-up and resend flow
  hands exactly one message to the transport — the link where an account awaits
  one, and the sentence that says no link was sent everywhere else: nobody has the
  address, the account cannot be sent a link, the account was already verified, a
  link mailed a moment ago still stands, the address is taken (`auth.no_link_requested`).
  While a transport refuses everything it refuses both callers alike, which is the
  only case a public door may speak of.
* **The forgotten-password flow's refusals are not public.** One link is the whole
  of what a known and an unknown forgot request cause between them
  (`TestTheMailDeliveryDoorDoesNotTellAKnownAddressFromAnUnknownOne`), so the
  no-account branch sends no mail and leaves no record, and a door that answered
  "that mail was refused" about one of those callers and "that call caused no
  mail" about the other would have said who has an account. Its `auth.set_password`
  row is still written and still reaches the trail; what is withheld is the public
  read of it, and the forgot form is answered by its neutral acknowledgment alone.
  Buying the word back costs a mail to every address that has nobody behind it.
* **What the door still leaks, exactly.** A transport that refuses one mailbox and
  takes another tells a fact about the address it refused, and any word about it
  names that address. Nothing about the selective case is airtight while it lasts:
  the only shape that closes it completely is never answering `failed` at all,
  which leaves the person with a promise of a link nobody sent. The shell's
  sentence is about the mail server for that reason.
* **A sign-up for an address that already has an account sends one message.** It
  says "somebody asked for a link to this address and no link was sent", the same
  sentence every other no-link branch of the verification flow says, and it is
  what lets the two sign-ups be answered from their own records. It is mail to a
  mailbox that asked to register, capped by the same `MayAsk` budget the sign-up
  itself is capped by — never more than the link it replaced would have cost.
* **A transient SMTP failure is now final for a direct send.** No outbox retry
  ladder and no dead letter: the row, the `auth.mail_failed` event and a person who
  can be told are what took the ladder's place. An invitation whose mail the server
  refused is therefore not retried, and the inviter learns nothing beyond the trail
  until the invited person's own screen says so.
* **No retention or erasure** of the addresses these rows record — the same
  question `notification_deliveries` lives with, and the product's to answer
  ([§ Object scope](#object-scope)).
* **The closed-port journey is not run by `make e2e`.** The harness boots one
  application whose mail points at the catcher; no second application boots with
  `mail.port` naming a port nothing listens on, so the `failed` half of the
  acceptance is proven in Go (`mail_delivery_test.go`, `refused_link_notice_test.go`)
  and the shell's reading of it is proven against a stubbed door
  (`e2e/email-verification-forms.spec.ts`), never against a live refused SMTP send.

## A SAML assertion at the same door

`Deps.SAMLProviders` mounts three app-surface routes — `auth-saml-start`,
`auth-saml-callback` and `auth-saml-metadata` — for a composition that can
resolve a tenant's identity provider at all, and no route at all for one that
cannot. The service provider is built per request from the row the `Host`
resolved and addressed at that host, so two tenants on one installation have two
entity IDs, two assertion consumer URLs and two metadata documents; what is kept
between requests is only the parsed IdP document, keyed by its own bytes and by
the URL it came from — a copy fetched from a URL for an hour and no longer, so an
IdP that rotates its signing certificate is followed within the hour instead of
leaving the tenant at 403 until somebody restarts the process. The assertion is
verified before anything is written: its own signature — a Response that signs its
envelope and not the assertion inside it is refused, which is *not* the library's
default and is the case `TestAnUnsignedSAMLAssertionIsRefused` holds at the door —
its audience against this tenant's entity ID, and an assertion that names no
audience at all names nobody and is refused, which is likewise *not* the library's
default (`TestASAMLAssertionWithNoAudienceRestrictionIsRefused`); its subject bound
in the bearer method, or by the schema's silence, because the library checks a
subject's recipient and request id for every confirmation it carries and for none
when it carries none
(`TestASAMLAssertionBoundToNoBearerIsRefused`); its recipient against this host's
ACS URL; its window with the library's 180-second skew; and the request it answers,
which is the browser's own tracking cookie and the reason IdP-initiated sign-in is
refused rather than allowed with less checking. The address the named attribute
carries
then runs the tenant's registration rule and `Service.Open`, the same call the
OIDC callback finishes with, so the second-factor rule is not re-expressed here
at all.

Decision 0022, for this delivery. **Reused:** `Service.Open` and everything
behind it — `ErrFactorRequired`, `markFirstFactorProved`, `refusedAtTheDoor`;
`users.ByEmail`, `ConfirmAddress` and `contracts.Provisioner`, which is how an
unknown address stays the tenant's decision rather than a protocol's;
`discover`'s cached-resolution-and-503 shape for metadata that cannot be fetched;
the hourly sweep's batched `purge` loop, one arm of which is the replay row's
expiry; `httpx.Register` with `httpx.Public()`, and `redirectOutput` for the two
legs. **Added:** the assertion consumer service and the per-tenant SP metadata
document, because the trace found no served-XML surface and no inbound-POST leg
anywhere to extend; the replay table `000047_saml_assertion_replays`, whose
primary key *is* the claim; `contracts.SAMLProvider` and its `SAMLProviders`
port, naming no SDK; the metadata copy's hour, and the fetch's deadline and
ceiling; and `Open`'s sign-in-method argument (`ViaOIDC`,
`ViaSAML`) — the port grew by the one value its own trail already carried,
because a login the log called "oidc" would send whoever reads it to the wrong
provider about the person they are asking about. **Made reusable, in the one sense
the word earns here:** `authtest.SAMLIdP`, the in-process identity provider that can
emit the unsigned, wrong-audience, wrong-recipient, no-audience and unconfirmed-subject
variant of an assertion by construction, so a refusal is tested against a document a
real IdP would send rather than a hand-written one. The two shapes the delivery also
leaned on are named as shapes and are not importable: the per-request service provider
(build it from the row, cache only the other party's document, key that cache by the
document and not by the tenant) and the external credential spent in the same
transaction as the session it buys live as this module's own `SAML` value and
`spendAssertion`, unexported, and a later module that wants one moves it out with a
consumer in front of it rather than reaching over.

What is deliberately not here: the service provider holds no key, so AuthnRequests
go unsigned and the metadata names no `KeyDescriptor` (`contracts.Secrets` is
where a key would arrive and a `saml_key_ref` column where it would land, neither
of which anything reads today); an encrypted assertion is refused rather than
parsed, and so is one bound holder-of-key or sender-vouches, for the same lack of
a key; there is no installation-level SAML default to fall back on, unlike
OIDC — the legs mount on the port alone; and there is no single logout, so
`auth.Logout` stands as the only way a session ends. Two things about it are named
rather than hidden. The assertion consumer service is an anonymous POST on the *app*
surface, so `kit/httpx`'s public-write limit — the Public surface's only limit — does
not reach it, and what bounds it is this module's own counter: sixty assertions an
address a minute (`contracts.AssertionsPerAddress`, the rate the kernel counts the
forms it does gate at), refused ahead of the base64, the parse and the signature so
the refusal costs less than the work it refuses — and one office behind one NAT is one
counter, which is the cost the kernel already accepts for the same reason. And the hour
a document fetched from a metadata URL is trusted for is both the cure and the
residual: a rotated-in certificate is followed within the hour, and a certificate its
owner has taken out of service stays trusted here for the same hour. And the
tracking cookie is `SameSite=None`, which a browser only sends over TLS: local
development needs a terminator in front of the callback, because no cookie
attribute makes the IdP's cross-site POST carry one over plain http — `Lax` is the
other half of the same refusal, which is why the attribute is not a knob to turn.

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
  is that read, and the read is asked of the answer as well as the prompt — a tenant
  that shut the door while a ceremony stood open is refused that ceremony
  (`TestDisablingPasskeySignInRefusesAnOutstandingCeremony`), because the setting is
  what may open a session here now rather than as at the moment a nonce was minted.
  The counter is written as a compare-and-set (`UPDATE … WHERE id = ? AND
  clone_warning = false AND sign_count = ?`), so two assertions carrying one counter
  settle on one accepted session rather than two, which is the clone rule surviving
  the interleaving a prompt-per-tab browser creates on its own
  (`TestConcurrentPasskeyAssertionsRejectARepeatedCounter`). And the refusal to take
  the last factor away counts the factors that can still answer: a credential retired
  as suspect is a row and not a way in, so it neither stands in for a usable factor
  when the last one is being withdrawn nor waits for a favour it cannot use
  (`TestASuspectPasskeyDoesNotPermitWithdrawingTheLastUsableFactor`,
  `TestARetiredPasskeyIsWithdrawnAsTheRowThatItIs`). What is *not* here is the administrator's control for that row (the
  product's), any page, and any attestation policy: this module holds no
  manufacturer registry, so which AAGUID made an authenticator stays a specialist
  fact rather than a shared module's table.
* **Bearer tokens are in** (the brief's item 4) — see the next section, and its
  own list of what is still open there.
* **No factor page, and the sign-in page answers a passkey, not a code.** The
  reference app's sign-in page carries the second step for the half that is a
  ceremony: `modules/admin/internal/pages.go` wires the refusal's own form to
  `/challenge/passkey/begin` and `/challenge/passkey/verify` and
  `ui/assets/js/passkeys.js` drives the prompt, which is what
  `e2e/passkey-second-factor-door.spec.ts` and `e2e/passkeys.spec.ts` walk in a
  browser. No page calls `/api/v1/auth/challenge/verify`: a TOTP or a recovery
  code is still answered over the JSON route, which works and is tested at its
  address — a correct code there is a 200 and a session cookie — while the page
  shows the refusal text and stops. The code-entry work is the shell's.

## Bearer tokens for a person's own integrations

A session cookie proves a browser. Nothing proved a script until
`000032_api_tokens.up.sql`: the only credential this platform accepted was a
cookie, so a mobile shell or a deploy bot had to hold a person's password and
impersonate their browser.

```
POST /api/v1/auth/tokens              SignedIn   mint a key; the token is in this response and nowhere else
GET  /api/v1/auth/tokens              SignedIn   the person's own keys: name, scopes, expiry, last use, revoked
POST /api/v1/auth/tokens/{id}/revoke  SignedIn   stop one key, leaving the person's own sessions alone
POST /api/v1/auth/tokens/revoke-self  BearerKey      the presented key retires itself; 204, no id, no grant
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
halves at the kernel, and `scoped_key_is_held_to_its_scope_test.go`
drives them at the routes.

The rule stops there, at the operations that spend it. This module's `POST
/tokens/revoke-self` declares `httpx.BearerKey()`, the single credential door a
key may spend, because what it spends is that key and nothing of its holder's
(below). One operation names no permission because there is none to name: `GET
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
stopped by id with `POST /tokens/{id}/revoke`, which is a session's work for the
same reason. The one exception is `POST /tokens/revoke-self`
(`auth-token-revoke-self`, `httpx.BearerKey()`): it takes no id, ends exactly
the key whose `tenancy.Principal.TokenID` authenticated the request, needs no
management grant, and answers 204. A key that can name only itself cannot reach
its holder's other keys, so a device can sign out by destroying the credential it
holds. A session is refused it with 403, and a dead or absent key is answered
401 `AUTH_ANONYMOUS` with a Bearer challenge before the handler. The catalogue
lists the command for a key on an `auth/token` entry with `"operations": []` —
nothing about keys is readable there — and lists nothing of it for a session. Called in-process for a key already gone, the command
answers `crud.ErrNotFound`, where `RevokeToken` answers success —
[ADR 0020](../../docs/adr/0020-a-credential-may-retire-itself.md) says why.

### Open here, stated rather than approximated

* `GET /api/v1/auth/me` answers a cookie session; a bearer caller is refused it
  by the kernel's rule above, ahead of the handler, and the 403 says which.
  Its body is the person's roles and everything they grant,
  which is wider than a scoped key may act as — reporting that to a narrowed
  caller would be a `/me` that lies about the caller's own authority. A
  "who am I, as this key" answer (the brief's `Identity.Token`) is unbuilt.
* Every route in this module but one is a session's work. A bearer key reaches
  the kernel's resource catalog, which names the resources the key's own scopes
  open, and this module's `POST /tokens/revoke-self`; it finds nothing else here
  to manage itself with — no `GET /api/v1/auth/tokens` to see what a holder minted.
* No page: keys are managed through the JSON routes.
* Uses are not audited, and `last_used_at` is the record a person reads; the
  trail keeps the two facts that mean something about a key, that it was made and
  that it was stopped.

## The two mails this module sends itself

Decision 0022, for this delivery. **Reused:** `events.Defer` for the hand-off,
`db.ConnFrom` for the connection a post-commit write needs (the same position
`forget` already used through `httpx.ConnFrom`), `pendingLinkOf` and the
compare-and-delete that protects a link the person already holds, `recordMail` and
its redaction unchanged, and `created_at` as the column both resend caps read: the
hand-off moved the send out of the minting transaction, but a cap that read a stamp
written by a *later* transaction would stop binding for the work still inside the
minting one, and one transaction that minted twice would mail twice. What answers a
refused send is the withdraw, not a column. **Made reusable:** `linkMail`, the one
shape of "everything read inside the transaction, only the send deferred", which any
future credential link in this module takes instead of rediscovering the ordering.
