# Notification module

`modules/notification` tells somebody something, in the application and
optionally by mail. Rows are addressed to a person, so the two routes under
`/api/v1/notification/notifications` are scoped by the principal rather than by
a permission, and the manifest declares neither permissions nor a navigation
entry; a subscription delivers the mail-marked rows through the composed
`Mailer`. Without a mail host the rows are still recorded and the mails are
logged, which is what a development machine wants.

Compose it with `notification.Deps{Recipients, Senders, Providers, Mailer, Hosts,
Secure}`: the mailer is `providers/gomail` — `gomail.New(gomail.Config{…})` over
`config.example.yaml`'s `mail` section (password through `PLATFORMKIT_MAIL_PASSWORD`),
the dependency the register names — or `NewMailbox()`, the in-memory one, and
`Senders` and `Providers` may each be nil — every nil is a named refusal inside the
decision rather than a surprise at send time, and a deployment with no `Senders` speaks
as the address in its own configuration for every tenant, which is what it did before
per-tenant senders existed. Consumers import [contracts/](contracts/) and its
[fake](contracts/notificationtest/), never `internal/`; mail templates live in
[internal/templates](internal/templates/).

Who may be told what is decided in the notice's own transaction, before any row is
written. `Service.Notify` reads the person's channel choices and quiet window
(`notification_preferences` and `notification_quiet_hours`, migration 000028) and the
tenant's sender (`notification_senders`, 000029), runs `contracts.Decide`, and accounts
for the outcome of every channel the notice asked for: a chosen one opens a `requested`
row and its own outbox event, a refused one closes in the same transaction with the
sentence a person would read and the `Correction` naming whose decision would stop it —
the recipient's, the tenant's, or the deployment's. `notification.Settings()` is the form
behind those choices, over the caller's own rows, and `notification.Senders(verifier, keys)`
the tenant's sending address, one row per tenant; neither is reached by a route here.

A provider in `Providers` is a channel this deployment sends, and a channel it sends has
somebody listening: `subscriptions(deps)` composes one carrier subscription per provider
(`internal.Carrier`), named by the same `contracts.RequestedEvent` table `Notify` asks that
channel by, so the two halves come from one `Deps` value and cannot disagree. The carrier
answers into a ledger row this module writes — nil is `sent`, `contracts.ErrPermanent` is
`failed` with its sentence, anything else rolls back and retries — and a provider wired for
`in_app` or `email`, which the module carries itself, panics at composition.

Mail leaves as the tenant that raised it. `SendMail` reads `notification_senders` in
the worker's own transaction and hands the row to the carrier, which puts the tenant's
name and address in the `From:` **header** and signs it with DKIM (RFC 6376) under the
row's selector; the **envelope** sender stays `mail.from` — one bare address, one per
binary, still refused by `kit/config` with a display name attached, because `MAIL FROM`
and `From:` answer different questions. A verified sender whose key this deployment does
not hold is never sent unsigned. What the relay answers decides the ledger: a 5xx is
`contracts.ErrPermanent`, which writes `failed` with the relay's own sentence and
acknowledges the event, and anything else writes no row, rolls back and is retried on the
kernel's ladder. A delivery the ledger already closed is not sent again — the worker reads
the terminal row before it dials, which is the other half of migration 000030.

The ledger is `notification_deliveries` (migration 000027), under the tenant's
row-level security and append-only: a `requested` row when the notice is written, then a
terminal row in the same transaction as the step that finishes the channel — `sent`, or
`suppressed` with the reason (no address, the notice deleted before its mail was due).
In-app is requested and sent with the row itself. A relay that refuses leaves no
terminal row, because its transaction rolls back and the outbox retries; a send that can
never succeed is recorded by the kernel in `platformkit_dead_letters`. Migration 000030
is a unique partial index over the `sent` rows, so a redelivered send is accounted once
and the worker acknowledges the retry instead of dead-lettering a delivery that
happened. `delivery_ledger_coverage` — requested channels with a terminal row, over all
requested channels — is `internal.Coverage`.

## Interface

`contracts/` is the whole surface: `Service` (notify, list, mark read), `PreferenceService`
and `Preferences` (one person's channel switches and quiet window), `Senders` and
`SenderAdmin` (the tenant's one sending address), `Decide` and the channel vocabulary, the
nine names in `Events`, and the ports the composition owes the module (`RecipientLookup`,
`HostLookup`, `Mailer`, `Senders`, `DKIMKeys`, `SenderVerifier`, `Provider`).
Consumers import that and `contracts/notificationtest` — a fake `Service`, a fake `Mailer`
and `RunService`, the suite the real service also runs — and never `internal/`. The two
faces the composition reaches are `notification.Settings()` and
`notification.Senders(verifier, keys)`; the two HTTP routes are hand-written, because a
`rest.Spec` list route is the whole tenant and these rows are addressed to a person.

There is no mobile counterpart to derive or compose here, and this module is why not: with
no `rest.Spec` there is no catalog resource and no command surface, so a phone client
holding this module's own contracts would have to be written against Go, not a generated
catalog (decision 0019). A bell derived from `Service` and `PreferenceService` is the form
that surface would take; it is the product's to build and the limits below name what stops.

## Composition

**Reused** — the delivery ledger and its one writer (`internal.record`,
`internal.Coverage`, migration 000027 with 000030's sent-once index), the channel
vocabulary and the single decision (`contracts.Channel`, `contracts.Wants`,
`contracts.Decide`), the outbox and its retry ladder (`kit/events`, `docs/adr/0004`),
row-level security through `platformkit_tenant_match`, and the `Mailer` port with its
second consumer in `modules/auth`, which keeps mailing a reset link that exists in no row.

**Added** — `providers/gomail`, because the brief names `github.com/wneessen/go-mail` as
this pillar's standard and the stdlib `net/smtp` conversation it replaced could neither
sign a message nor carry a tenant's own header nor bound a wedged relay; and the worker's
reading of the tenant's sender, its `contracts.ErrPermanent` split, and its skip over a
closed delivery, because "a tenant sends from its own name" and "a suppressed notice has a
row saying so" are both facts about the send and no existing unit reached the send.

**Made reusable** — `providers/` as the place a carrier lives outside `internal/` (the
`kit/events/providers/{memory,nats}` pattern, one package per SDK, so the module links no
vendor library and a deployment that sends nothing imports nothing), `contracts.Permanent`
as the one error a provider returns when another attempt would say the same thing, and
and the shape of a provider test — an in-process relay in the carrier's own package that
asserts what went on the wire rather than what a mock said (`providers/gomail`, whose relay
lives in `gomail_test.go` and is therefore a pattern to copy, not a package to import).

## Authorization

### Permissions

One key: `sender:manage` (`contracts.PermissionSenderManage`, declared in `permissions` in `modules/notification/module.go`). It is the tenant's own sending address — `Put`, `Verify` and `Delete` in `internal/senders.go` — and it is defined here rather than beside a route because `kit/app` refuses a route whose permission no manifest defines.
No nav entry names it: a nav entry decides who sees a link, and this key guards a mail identity, not a page.
The module's two routes stay outside it, guarded by `httpx.SignedIn()` in `modules/notification/internal/handler.go`: `GET /notifications` (`notification-notification-list`) and `POST /notifications/{id}/read` (`notification-notification-read`) — both are about the caller, and a permission every signed-in person must hold decides nothing.
The `SendMail` event subscription (`modules/notification/internal/mail.go`), and the one carrier subscription per wired provider (`internal.Carrier`), are run by the kernel and are not guarded by a permission.

### Object scope

None. No call to `tenancy.Policy` and no `Resource.Kind` appears in the module.
Scope is the caller: `caller` reads the principal's `UserID` from `tenancy.PrincipalFrom`, never from a parameter, and `ListFor` and `MarkRead` take that id.

### Duties the module enforces itself

`Service.MarkRead` in `modules/notification/internal/service.go` answers `crud.ErrNotFound` when the row's `RecipientID` is not the caller, so nobody learns whether another person's notification exists.
`caller` in `handler.go` answers 403 when there is no principal or the user id is nil.
This is a recipient check, not a separation-of-duties rule.

### Public faces

None. The module mounts its routes on the app surface only (`internal.RegisterRoutes(s.App, svc)`), and no `httpx.Public()` route exists.
It has no public write, so `kit/limit` is not used.

### The operator boundary

None. `sender:manage` is not marked `Operator: true` — its scope is the tenant's own row, which the transaction resolves, so an operator is not the only one who may hold it — and no `OperatorRead` or `OperatorWrite` route exists.

### Provisioning

`sender:manage` is the one key a role would be granted, and it is granted by nothing today, because the module mounts no route over the sender face: the composition holds `Senders(verifier, keys)` and the product writes the page. Granting it is therefore the product's act over its own roles, and until it does, the commands are reachable only from inside the composition.
Every signed-in person can read and mark their own notifications and set their own channel switches without any grant: those commands are scoped by the principal, not by a key.
Roles and the roles API therefore do not affect this module's two routes.

## Limits

What this module does not do, stated here rather than only in the branch that left it out:

- **No in-app bell and no list in the kernel shell.** `Service.ListFor` and `MarkRead` and
  the two routes answer the question a bell asks; nobody renders it. There is no nav entry
  and no page, so a `requested` in-app row is read only by a client that already knows the path.
- **No settings or sender page, and no route over either face.** `Settings()` and
  `Senders(verifier, keys)` are the commands and `sender:manage` is the key; the product
  writes the form and the route, and until it does a person cannot change a channel switch
  from a screen and an administrator cannot set a tenant's sender from anywhere but Go.
- **No templates per event per locale, and no HTML alternative.** `internal/templates` holds
  one text template, `contracts.Message.HTML` and `Lang` are read by the carrier and written
  by nobody, and `internal/mail.go` sets `Lang: "en"`; locale is the product's copy table.
- **Mail is the only carrier that reaches a device.** The module can host a carrier for any
  channel — `internal.Carrier` asks it and closes the ledger row around the answer — and
  `contracts.Provider` is the shape, but nothing in this repository implements it: a wired
  push carrier gets `Delivery.Target` empty, because device tokens, browser subscriptions
  and tenant endpoint URLs live outside this module.
- **No expected revision.** No command in this module takes one, as none in the reference
  module does; the three sender commands read the row under `FOR UPDATE`, which is where a
  lost update would matter. `Put`'s lock is asserted by no test here, and none can be
  written in this package: the window it closes lies between that command's own read and
  its write, and an ask from another transaction can only reach the row after the write,
  where the row is locked whichever way the read went.
- **Not measured as an evidence indicator.** `internal.Coverage` computes
  `delivery_ledger_coverage` as requested channels with a terminal row over all requested
  ones; the repository has no `tools/pillars.py` to report it through, so the ratio is a
  query and a number in the brief, not a dashboard.

## Verification

`go test ./modules/notification/...` runs the contracts suite against the real service,
a real Postgres and a real tenant transaction (`TestServiceConforms`), the settings and
sender commands against the same (`TestAConversationWithTheSenderCommands`), a redelivered
send against 000030 (`TestARedeliveredSendWritesOneSentRow`), one tenant's writes against
another tenant's rows with a caller both transactions name (`TestOneTenantsLedgerIsNotAnothers`,
`TestAnotherTenantThatHoldsTheKeyWritesNothingOfThisTenants`), the sender commands against a
caller that names nobody (`TestTheSenderCommandsRefuseACallerWhoIsNobody`) and a wired
carrier through its ledger row
(`TestAChannelTheDeploymentWiresACarrierForReachesATerminalRow`).
`TestAnotherTenantWritesNothingOfThisTenants` is the same cross-tenant conversation with an
actorless fixture, and it stands red for that reason alone: its first line is a `Put` by a
transaction that names no caller, which the rule above refuses before any of its isolation
assertions is reached. The case named above makes the same attempts with a caller named —
which is the assertion that the separation comes from the row's policy and not from who was
asking.
`providers/gomail`'s assertions run against an in-process relay in its own package, so what
a real relay answers — DKIM as the far end verifies it, STARTTLS negotiation, AUTH — is
observed by no test here.
