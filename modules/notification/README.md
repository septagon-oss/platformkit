# Notification module

`modules/notification` tells somebody something, in the application and
optionally by mail. Rows are addressed to a person, so the two routes under
`/api/v1/notification/notifications` are scoped by the principal rather than by
a permission, and the manifest declares one permission — `sender:manage`, which
guards the tenant's sender commands and no page — and no navigation entry. A
notice that asks for mail is answered twice: a subscription decides the mail and
writes it as an intent, and a job sends it after that decision commits, so the
socket a notice's mail opens is never inside a database transaction. Without a
mail host the rows are still recorded and the owed mail closes `suppressed` on
the delivery ledger with the reason a reader can act on, which is what a
development machine wants.

Compose it with `notification.Deps{Recipients, Senders, Providers, Mailer, Hosts,
Secure}`: the mailer is `providers/gomail` — `gomail.New(gomail.Config{…})` over
`config.example.yaml`'s `mail` section, with the password read through `kit/secrets`
under the name `mail.password` — `PLATFORMKIT_MAIL_PASSWORD` on a laptop, a mounted
file or an OpenBao mount in a deployment that has one —
the dependency the register names — or `NewMailbox()`, the in-memory one, and
`Senders` and `Providers` may each be nil — every nil is a named refusal inside the
decision rather than a surprise at send time, and a deployment with no `Senders` speaks
as the address in its own configuration for every tenant, which is what it did before
per-tenant senders existed. Consumers import [contracts/](contracts/) and its
[fake](contracts/notificationtest/), never `internal/`; mail templates live in
[internal/templates](internal/templates/).

Who may be told what is decided in the notice's own transaction, before any row is
written. `Service.Notify` reads the person's channel choices and quiet window
(`notification_preferences` and `notification_quiet_hours`, migration 000054) and the
tenant's sender (`notification_senders`, 000055), runs `contracts.Decide`, and accounts
for the outcome of every channel the notice asked for: a chosen one opens a `requested`
row and its own outbox event, a refused one closes in the same transaction with the
sentence a person would read and the `Correction` naming whose decision would stop it —
the recipient's, the tenant's, or the deployment's. `notification.Settings()` is the form
behind those choices, over the caller's own rows, and `notification.Senders(verifier, keys)`
the tenant's sending address, one row per tenant; neither is reached by a route here.

A provider in `Providers` is a channel this deployment sends, and a channel it sends has
somebody listening: `subscriptions(deps)` composes one carrier subscription per provider
(`internal.Carrier`), named by the same `contracts.RequestedEvent` table `Notify` asks that
channel by, so the two halves come from one `Deps` value and one reading of it —
`contracts.Providers.Active`, which counts a key wired to nil as the absent channel it
names — and cannot disagree. The carrier
answers into a ledger row this module writes — nil is `sent`, `contracts.ErrPermanent` is
`failed` with its sentence, anything else rolls back and retries — and a provider wired for
`in_app` or `email`, which the module carries itself, panics at composition.

Mail leaves as the tenant that raised it, and that tenant is read twice. `SendMail`
reads `notification_senders` as it decides whether the notice owes a mail, so a sender
that is not verified owes no send at all, and the worker reads the same row again before
the socket opens, because the administrator can put a different domain there in between.
The composed `Mailer` puts the tenant's name and address in the `From:` **header** and
signs it with DKIM (RFC 6376) under the row's selector; the **envelope** sender stays
`mail.from` — one address per binary, which `kit/config` accepts with a display name and
the carrier strips for `MAIL FROM` and keeps in `From:`, because those two answer
different questions. A verified sender whose key this deployment does not hold is never
sent unsigned.

What the relay answers decides both rows, in one transaction after the send, and the
carrier names which answer it is giving. `contracts.ErrPermanent` or
`contracts.ErrNotAccepted` is a refusal the relay actually gave: the intent goes back
`queued` behind the module's own ladder — one minute, then ten — and ends `failed`
beside the notice's `failed` ledger row when the ladder or `contracts.MailIntentWindow`
runs out. An answer that never came back is `contracts.ErrUncertainSend`: the intent
becomes `unknown`, waits a day, is worth one more socket, and writes no ledger row,
because a ledger row says at most what this process can prove and this process cannot
prove the mail left. A mail the ledger already closed is not sent again — the
subscription reads the terminal row before it writes the intent, which is the other half
of migration 000056 — and one mail request is one intent because 000076 makes
`(tenant, purpose, cause)` unique in the table, which is what a redelivered or a replayed
event resolves to instead of a second copy of one message.

The intent is `notification_mail_intents` (migration 000076), under the tenant's
row-level security with `FORCE`: the six states `contracts` spells in Go (`queued`,
`in_flight`, `sent`, `failed`, `suppressed`, `unknown`), an attempt count only a claim
raises, a two-minute lease, and a sweep that forgets a finished mail after
`contracts.MailIntentRetentionDays` and never an unfinished one. `internal.Deliver` runs
it per tenant: it claims the due rows in one short transaction that commits before any
socket opens, asks each row once more whether it is still wanted — the notice still
there, the recipient still at the address that was decided for, the tenant's sender
still verified, each refusal a `suppressed` row carrying its own sentence — sends with no
transaction and no lock held, and writes the answer guarded on the state it read, so a
worker whose relay answered late cannot overwrite a newer answer. An `in_flight` row
whose lease expired is the crash case: it becomes `unknown` rather than being re-sent on
the strength of the lease.

The ledger is `notification_deliveries` (migration 000027), under the tenant's
row-level security and append-only: a `requested` row when the notice is written, then a
terminal row in the same transaction as the step that finishes the channel — `sent`, or
`suppressed` with the reason (no address, the notice deleted before its mail was due).
In-app is requested and sent with the row itself. A carried channel whose relay fails
without refusing it leaves no terminal row, because its transaction rolls back and the
outbox retries; a send that can never succeed is recorded by the kernel in
`platformkit_dead_letters`. A notice's mail is not one of those: its terminal row arrives
in the intent worker's outcome transaction, and the outbox's ladder is not what retries a
mail. Migration 000056 is a unique partial index over the `sent` rows, so a redelivered
send is accounted once
and the worker acknowledges the retry instead of dead-lettering a delivery that
happened. `delivery_ledger_coverage` — requested channels with a terminal row, over all
requested channels — is `internal.Coverage`.

The mails a composition sends *outside* a notice are recorded elsewhere, in
`direct_mail_deliveries` (migration 000044) and through
[`contracts.MailLedger`](contracts/mail.go): a set-password link and a
verification link are minted by `modules/auth` and handed to `contracts.Mailer`
directly, so the secret is in the message and in no row, and until this table
existed neither the attempt nor its outcome was written down anywhere. One row
per send, appended in the transaction of the step that decided it, saying who it
went to, which kind it was (`auth.set_password`, `auth.verification` — the sender
names its own kinds), what became of it (`sent`, `suppressed`, `failed` with the
transport's own words redacted by `contracts.RedactMailReason`), and which call
asked. It holds no subject, body, link or credential. A credential mail does not come
000076's way and could not: an intent keeps the message it owes verbatim until it is
sent, which is what makes sending after the commit possible and is exactly what a live
secret must not do — so the link is minted and mailed in one breath over
`contracts.Mailer.Send`, and this table is the only record of the attempt.

**`delivery_ledger_coverage` does not count these rows and never will**: the
register's number is requested channels of notices with a terminal row, and a
mail with no notice and no `requested` half would silently narrow what that name
means. Ask the two tables two questions.

### Limits

* **No retry ladder on this side.** A notice's channel that fails is retried by
  the outbox and dead-letters — its mail by the intent worker's ladder above, which is
  a capability of its own and not this table's; a direct send that fails is
  acknowledged by the module that made it, so its `failed` row and its
  `auth.mail_failed` event commit in the transaction of the attempt. That is the trade
  the ledger is built on — a
  row that commits with the step it describes or not at all — and what it gives up
  is named: a transient transport failure on a set-password or verification link is
  final for that attempt, and the retry is the person asking again, not the queue.
* **`sent` means "this process proved it", not "it arrived".** The row commits in
  the transaction that minted the credential; a transaction that aborts after the
  transport took the message leaves no `sent` row, so the table under-reports a
  mail that really went rather than over-reporting one that did not. No `requested`
  row exists to catch the difference, on purpose.
* **No retention, no export.** The screen exists — `GET /app/notification/notifications`
  lists the caller's own rows, live — and it writes nothing. Rows are appended and
  nothing in this schema deletes or updates them, and the table holds a recipient
  address, so
  "what happens to these rows when a person is forgotten" is unanswered here —
  the platform has no cross-table erasure, and the product that promises to forget
  people owes that answer. Its one read is used by one public door in
  `modules/auth`; `Coverage` ignores the table.
* **One read, by the id of the call that asked.** `MailOutcome` answers one request
  id, newest-first, and nothing else: no listing, no range, no per-address
  history, and no method answering "what is the transport doing" from the tenant's
  newest row. That last one existed until review 3 and is gone because it could not
  be bounded: the door answered a caller whose own call left no record with whatever
  refusal the tenant held most recently, and sign-up is public, so an attacker can
  register a mailbox its relay rejects and refresh that refusal before every probe.
* **The read takes the kinds to answer among, and holds no list of them.** `MailOutcome(id, kinds...)`
  answers as if no row of any other kind existed, and answers about every mail of
  that call when given none; a kind the caller does not name is answered exactly as
  a call that mailed nothing is. Which kinds a caller names is the caller's business
  — modules/notification has no list of mail kinds and a CHECK over one module's
  names would be a schema change every time a module mailed something new — and the
  reason a caller narrows the read at all is privacy rather than convenience: see
  `contracts.MailReport` and `modules/auth/internal/email_registration.go`.

## Interface

`contracts/` is the whole surface: `Service` (notify, list, mark read), `PreferenceService`
and `Preferences` (one person's channel switches and quiet window), `Senders` and
`SenderAdmin` (the tenant's one sending address), `DeviceService` (the devices a
person registered), `Decide` and the channel vocabulary, the
twelve names in `Events`, the mail this module owes (`MailIntents`, `OutgoingMail`,
`MailIntent` and its six states, `PrepareIntent`, and the two answers a transport gives
that no status code carries, `ErrNotAccepted` and `ErrUncertainSend`), and the ports the
composition owes the module (`RecipientLookup`, `HostLookup`, `Mailer`, `Senders`,
`DKIMKeys`, `SenderVerifier`, `GrantChecker`, `Provider`). The intent reaches its caller
through the plan as `contracts.MailIntents`, which this module Provides, and
`Deps.Tenants` — a `jobs.TenantLister` — is what lets its two jobs run per tenant.
Consumers import that and `contracts/notificationtest` — a fake `Service`, a fake
`Mailer`, a fake `MailIntents` whose `Owed()` is what a caller is owed, and `RunService`
and `RunMailIntents`, the suites the real service and the real table also run — and never
`internal/`. The three faces the composition reaches are `notification.Settings()`,
`notification.Devices(adapters)` and
`notification.Senders(verifier, keys, grants)`; the five HTTP routes are hand-written, because a
`rest.Spec` list route is the whole tenant and these rows are addressed to a person.

There is no mobile counterpart to derive or compose here, and this module is why not: with
no `rest.Spec` there is no catalog resource and no command surface, so a phone client
holding this module's own contracts would have to be written against Go, not a generated
catalog (decision 0019). A bell derived from `Service` and `PreferenceService` is the form
that surface would take; it is the product's to build and the limits below name what stops.

## Composition

**Reused** — the delivery ledger and its one writer (`internal.record`,
`internal.Coverage`, migration 000027 with 000056's sent-once index), the channel
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
`kit/events/providers/{memory,nats}` pattern, one package per SDK, so nothing in
`contracts/` or `internal/` names one), `contracts.Permanent`
as the one error a provider returns when another attempt would say the same thing, and
and the shape of a provider test — an in-process relay in the carrier's own package that
asserts what went on the wire rather than what a mock said (`providers/gomail`, whose relay
lives in `gomail_test.go`: a testing approach to copy, not an API to import).

**Reused, by the in-app list** — `kit/stream` and the connection the kernel mounts at
`/api/v1/app/stream`, `module.Stream` with `module.StreamRecipient("recipientId")` as the
manifest's own door onto the two events this module already raised, `ui/page`'s fragment
answer for the htmx fetch of a region, `ui/components` for the toolbar, table, empty state
and pager, and the hand-mounted-page shape `modules/change` drew for rows that belong to a
person rather than to a tenant.

**Added, by the in-app list** — `internal.MountInApp`, because a `rest.Spec`'s list route is
the whole tenant and these rows are the caller's, so no generated screen could carry them at
any setting of its fields; and `notification.Page`, the shell the list is drawn in, because
which chrome a person's own notices sit in is an installation's fact and this module's silence
when it is not handed one is the answer a deployment with no such page expects.

**Made reusable, by the in-app list** — `document.View.Live`, one field on a page that any
module's own screen may set to go live, with the wrapper, the pinned extension's tag, the
controller the region runs on and the fragment answer drawn by the kernel rather than by the
module. What stays this module's own is the one slice its manifest's `Streams` and its page's
topics are read from: nothing exports that rule, and a screen which watched a topic its
module never opened would be watching a door no module opened.

**Reused, by the delivery answer** — `contracts.Mailer` and both of its carriers, unchanged,
with a non-nil one still in every composition; `notification.Mailbox`, still the record a
mail-less deployment runs and a test reads, and no longer a promise; and the
second-interface-beside-a-frozen-one unit `modules/site` opens with `LockedReader`, with
`MailDeliveryFunc` answering the way `PaletteFunc` does.

**Added, by the delivery answer** — `contracts.MailDelivery`, because a composition holds a
`Mailer` whatever the deployment chose and "there is a mailer" therefore answers nothing
about whether a message leaves: the caller that acted as though it did accepted sign-ups it
could never mail. A method on `Mailer` would have said the same thing and is the break the
frozen public line refuses, so it is a second interface and the carrier answers it about
itself — `*gomail.Sender` dials, `*Mailbox` files — and one object is put twice, so no
composition can hold a sender and a delivery answer that contradict each other.

**Made reusable, by the delivery answer** — the carrier answering a question about itself
beside the interface it already satisfied, which is how the next "does this installation
really do the thing?" is asked without widening a frozen port; and `contracts.MailDeliveryFunc`,
so a composition or a fixture can say which installation it is in one token.


**Reused, by the mail intent** — the delivery ledger and its one writer
(`internal.record`, and with it 000056's "a channel is sent once" partial unique
index), `contracts.Message` as the frozen rendered mail, `contracts.PrepareMail`'s
validate-before-write shape in the new `contracts.PrepareIntent`,
`kit/jobs.PerTenant` over `jobs.TenantLister`, `platformkit_tenant_match` with
`ENABLE`+`FORCE`, and the claim/commit/send/record job `modules/webhook` already
runs — the same four steps, the same reason for splitting them.

**Added, by the mail intent** — `contracts.MailIntents`, `notification_mail_intents`
(migration `000076`) and `internal.Deliver`. Nothing existing could carry this:
`notification_deliveries` is keyed by `notification_id` and begins at `requested`,
`direct_mail_deliveries` is written once at the decision and has no pending half,
and neither can hold a row that begins before the send is decided — which is the
only kind of row that survives a crash between a relay and a commit. `Mailer.Send`
is untouched: it still sends immediately for whoever composes it, and it is not
deprecated, because an intent is a different capability and not a newer way to do
the same thing.

**Made reusable, by the mail intent** — the owed-mail row and its worker are the
pattern for any effect this module must perform after the transaction that decided
it: `contracts.MailIntents.Queue(ctx, tx, OutgoingMail)` takes the caller's own
transaction, so a caller that cannot afford to lose a send writes one beside its
own state and adopts the same states, ladder, lease and unknown rule rather than
inventing a queue. `contracts.ErrNotAccepted` and `contracts.ErrUncertainSend` are
the two answers any carrier can give that no status code carries, and
`internal/mail_retention.go` is the forget-terminal-rows-never-owed-ones sweep.

## Authorization

### Permissions

One key: `sender:manage` (`contracts.PermissionSenderManage`, declared in `permissions` in `modules/notification/module.go`). It is the tenant's own sending address — `Put`, `Verify` and `Delete` in `internal/senders.go` — and it is defined here rather than beside a route because `kit/app` refuses a route whose permission no manifest defines. Its `Label` — `manage the tenant's sending address` — is the grant in words, which is what a refusal reads out and what `kit/module.Validate` refuses a permission for lacking.
The key is checked inside the transaction, not only at a route: `Senders.administered` asks the composition's `contracts.GrantChecker` whether the caller's roles grant it, and refuses when the deployment wires no checker to answer. A deployment that grants it to nobody therefore has working sender commands refused, which is the failure it will notice.
No nav entry names it: a nav entry decides who sees a link, and this key guards a mail identity, not a page.
The module's two notification routes stay outside it, guarded by `httpx.SignedIn()` in `modules/notification/internal/handler.go`: `GET /notifications` (`notification-notification-list`) and `POST /notifications/{id}/read` (`notification-notification-read`) — both are about the caller, and a permission every signed-in person must hold decides nothing. So do the three device routes in `internal/handler_devices.go` — `GET /notifications/devices` (`notification-device-list`), `POST /notifications/devices` (`notification-device-register`) and `POST /notifications/devices/{id}/remove` (`notification-device-remove`) — for the same reason in the same shape: the caller is the person the rows name, and `internal.owning` rechecks that inside the transaction each command writes in, refusing a principal that is not the recipient it was handed.
The `SendMail` event subscription (`modules/notification/internal/mail.go`), and the one carrier subscription per wired provider (`internal.Carrier`), are run by the kernel and are not guarded by a permission.

### Object scope

None. No call to `tenancy.Policy` and no `Resource.Kind` appears in the module.
Scope is the caller: `caller` reads the principal's `UserID` from `tenancy.PrincipalFrom`, never from a parameter, and `ListFor` and `MarkRead` take that id.
`notification_devices` (migration 000058) is the same fact one level down: `RegisterDevice` writes the caller, `ListDevices` sets the recipient filter rather than reading it from the query, and `RemoveDevice` answers `crud.ErrNotFound` for a row that is not the caller's live device — somebody else's, an already-removed one and one that never existed are one answer, which is what lets the route stay `SignedIn`.
`MailLedger` is written by the module that sent the mail, inside that module's own tenant transaction, and read one way — by the request id of the caller's own call — bounded to that tenant by the table's own row-level security; `direct_mail_deliveries` has no route and no permission of its own.
`notification_mail_intents` (migration 000076) is bounded one level further down, by `FORCE ROW LEVEL SECURITY` over `platformkit_tenant_match`: a transaction of another tenant that names one of its ids updates no row, and the worker runs per tenant, so the transaction that claims a row is that tenant's own and can resolve nobody else's.

### Duties the module enforces itself

`Service.MarkRead` in `modules/notification/internal/service.go` answers `crud.ErrNotFound` when the row's `RecipientID` is not the caller, so nobody learns whether another person's notification exists.
`caller` in `handler.go` answers 403 when there is no principal or the user id is nil.
`SetChannel`, `SetQuietHours`, `ClearQuietHours` and `Mine` in `internal/preferences.go` answer `tenancy.ErrPolicyDenied` when the context carries a principal who is not the recipient they name: one person's opt-out, quiet window and the list of both are their own, and row-level security cannot say so because both people are in one tenant.
Both halves of the mail path re-read the tenant's sender: the subscription when it decides whether a notice owes a mail at all (`internal/mail.go`), and the worker before the socket opens (`internal/mail_deliver.go`) — a sender that went back to pending after the mail was decided is a suppressed row, not a message signed as a domain that stopped vouching for itself. The worker re-reads the recipient's address by the same rule and suppresses a mail whose person moved, so a queued mail is never delivered somewhere it was never decided for.
This is a recipient check, not a separation-of-duties rule.

### Public faces

None. The module mounts its routes on the app surface only (`internal.RegisterRoutes(s.App, svc)`), and no `httpx.Public()` route exists.
It has no public write, so `kit/limit` is not used.

### The operator boundary

None. `sender:manage` is not marked `Operator: true` — its scope is the tenant's own row, which the transaction resolves, so an operator is not the only one who may hold it — and no `OperatorRead` or `OperatorWrite` route exists.

### Provisioning

`sender:manage` is the one key a role would be granted, and it is granted by nothing today, because the module mounts no route over the sender face: the composition holds `Senders(verifier, keys, grants)` and the product writes the page. Granting it is therefore the product's act over its own roles, and until it does, the commands are reachable only from inside the composition — and a signed-in caller needs a `contracts.GrantChecker` wired over the roles table before any of them is reachable at all.
Every signed-in person can read and mark their own notifications and set their own channel switches without any grant: those commands are scoped by the principal, not by a key.
Roles and the roles API therefore do not affect this module's two routes.

## Limits

What this module does not do, stated here rather than only in the branch that left it out:

- **No unread bell and no way in from the shell.** `GET /app/notification/notifications`
  is this module's one screen, and it is handed a shell rather than claiming one: there is
  no nav entry, no count in the shell and no link to the page anywhere in this repository,
  so a person reaches it from an address the composition wrote. The count a bell asks is
  `ListFor`'s answer and nobody draws it.
- **No settings or sender page, and no route over either face.** `Settings()` and
  `Senders(verifier, keys, grants)` are the commands and `sender:manage` is the key; the product
  writes the form and the route, and until it does a person cannot change a channel switch
  from a screen and an administrator cannot set a tenant's sender from anywhere but Go. The
  same is true of the grant: nothing in this repository implements `contracts.GrantChecker`,
  whose implementation is a few lines over the auth module's `Permissions`.
- **No templates per event per locale.** `internal/templates` holds one notice in two parts:
  the plain text and its HTML alternative, which carries the same words and the link as an
  anchor on the tenant's host. `internal/mail.go` sets `Lang: "en"`; locale is the product's
  copy table. The other channels' carriers are handed the text alone: `contracts.Delivery.HTML`
  is written by nobody, because no provider in this repository shows HTML.
- **Mail and push are the carriers that reach a device.** The module can host a carrier
  for any channel — `internal.Carrier` asks it and closes the ledger row around the answer —
  and `contracts.Provider` is the shape. `Deps.Push` is how a `kit/push` adapter becomes two
  of those providers: the channel a platform reaches gets `internal.PushCarrier`, which
  reads this person's live rows in the delivery's own transaction, sends one message per
  device, deletes a row the provider reports gone beside the ledger row that closes the
  channel, and publishes `notification.device_removed` with no actor because no person did
  it. `ChannelWebhook` is carried by nobody here: a tenant's endpoint URL, its HMAC secret
  and the product's decision to accept one are outside this module, and
  `contracts.Delivery.Secret` waits for them.
- **No device screen, and no composition that carries one.** The three routes
  answer a client that already knows them; nothing renders them here, and
  `apps/platformkit` wires no `push.Adapters`, so its registrations are refused
  with "this installation carries no web push" until the product picks a platform
  and hands over a VAPID pair. The person's own tokens are the export this table
  owes, and erasure is not: a product that promises to forget somebody must delete
  `notification_devices` by `(tenant_id, recipient_id)`, which no command here does.
- **One expected revision, on one row.** `Put` refuses a copy that names a `Revision` the
  row has gone past — `crud.ErrConflict`, nothing written, published or returned; migration
  000057 keeps the counter and `Verify` advances it. A copy naming none (`Revision 0`, a
  value built rather than read) states what the tenant's sender is and writes. That guard
  reaches the command and no further: `kit/entity`'s `Base` carries no version and
  `rest` takes no `If-Match` (docs/adr/0016), so a screen over `Senders()` has to carry the
  number in its form, and a token a caller hands in is discarded rather than checked: `Put`
  mints the verification challenge and no command accepts one.
- **Not measured as an evidence indicator.** `internal.Coverage` computes
  `delivery_ledger_coverage` as requested channels with a terminal row over all requested
  ones; the repository has no `tools/pillars.py` to report it through, so the ratio is a
  query and a number in the brief, not a dashboard.

## Verification

`go test ./modules/notification/...` runs the contracts suite against the real service,
a real Postgres and a real tenant transaction (`TestServiceConforms`), the settings and
sender commands against the same (`TestAConversationWithTheSenderCommands`), a redelivered
send against 000056 (`TestARedeliveredSendWritesOneSentRow`) and the same committed replay
over every asynchronous channel (`TestEveryCarriedChannelIgnoresACommittedEventReplay`), a
wired carrier through its ledger row
(`TestAChannelTheDeploymentWiresACarrierForReachesATerminalRow`), one tenant's writes against another tenant's rows with a caller both transactions name
(`TestOneTenantsLedgerIsNotAnothers`, `TestAnotherTenantWritesNothingOfThisTenants`,
`TestAnotherTenantThatHoldsTheKeyWritesNothingOfThisTenants`), two verifications of one
sender against one provider call and one event
(`TestConcurrentSenderVerificationChecksAndPublishesOnce`), the sender commands against a
caller that names nobody (`TestTheSenderCommandsRefuseACallerWhoIsNobody`), a member whose
roles do not grant `sender:manage` (`TestSenderCommandsRefuseAMemberWithoutTheManageGrant`),
a sender deletion's audit event (`TestDeletingAPendingSenderPublishesItsAuditEvent`), a
queued mail whose sender is no longer verified
(`TestQueuedMailRechecksSenderVerificationBeforeDelivery`), one recipient's choices and
their own list against another recipient's principal
(`TestPreferenceCommandsRefuseAnotherRecipientsChoices`, `TestMineAnswersOnlyTheCallersOwnChoices`),
every answer the sender commands can get from the composition's `GrantChecker`
(`TestSenderCommandsFollowTheGrantCheckersAnswer`), and two first answers for one
channel leaving one row (`TestTwoFirstAnswersForOneChannelLeaveOneRow`), a stale copy of a
sender against the identity a newer one committed
(`TestAStaleSenderEditPreservesTheCommittedIdentity`), another tenant's copy of a published
DNS challenge (`TestAnotherTenantCannotReuseAPublishedDomainProof`) and a carrier wired to
nil (`TestAnAbsentProviderClosesItsChannelAsSuppressed`).
The mail intent is checked against a real Postgres by the same suite its fake runs
(`TestTheMailIntentServiceOwesWhatItIsTold`), by the four answers a queue owes — a
decision that rolls back owes nothing (`TestARolledBackMailDecisionOwesNothing`), a
committed intent survives a restart and sends (`TestACommittedIntentSurvivesRestartAndSends`),
one mail request written twice owes one intent
(`TestTheSameMailRequestWrittenTwiceOwesOneIntent`), and another tenant reaches none of
this tenant's owed mail read (`TestAnotherTenantsOwedMailIsNotThere`) or written
(`TestAnotherTenantCannotWriteAnOwedMail`) — and by the two a relay can leave unanswered:
an ambiguous send is `unknown` and is not sent again within the delay
(`TestAnAmbiguousSendIsUnknownAndIsNotSentAgainWithinTheDelay`) and an attempt whose
lease expired is `unknown` rather than blindly re-sent
(`TestAnExpiredAttemptIsUnknownAndNotBlindlyReSent`). The address that moved is
`TestAMailIsNotSentToAnAddressNobodyDecidedFor`, the notice deleted before its mail was
due is `TestANoticeDeletedBeforeItsMailIsSentIsSuppressed`, and the sweep that forgets a
sent mail and never an unknown one is
`TestTheRetentionSweepForgetsASentMailAndNeverAnUnknownOne`.
`providers/gomail` runs against an in-process relay in its own package, including
`TestConfiguredCredentialsAuthenticateBeforeMail`, which refuses a relay the credentials
never authenticated to. What a real relay answers — DKIM as the far end verifies it, and
STARTTLS negotiation — is observed by no test here.

Four cases read the device table:
`TestARegisteredDeviceReceivesThePersonsPushNotice` (a device registered over its own
route, the delivery event reaching the composed adapter with the recipient and the
tenant on it), `TestAPushNoticeReachesTheCarrierAndAGoneDeviceIsRemoved` (the gone
answer closes the ledger row `failed`, deletes the row and publishes the removal with no
actor), `TestAnotherPersonsDeviceIsNotTheirsToRemove` (a member of the same tenant, a
command naming no person and a person of another tenant refused without writing,
publishing or losing a row) and `TestATokenReregisteredByAnotherPersonBecomesTheirs`
(one live token is one row). Against them: no device screen, so no browser journey, and
no live push service, so a provider's gone answer is a stand-in's.

The list page is checked beside those: the declaration the live door asks before a byte
leaves (`TestTheModuleOpensItsTwoNoticeEventsToTheirRecipientOnly`,
`TestTheDeclarationPassesTheKernelsOwnValidation`) and the address the pager links,
which is the address the page is mounted at
(`TestTheInboxPagerLinksTheAddressThePageIsMountedAt`). The drawing of a live page, the
shape of a refresh and the files a region runs on are the kernel's cases, in `./ui/document`
and `./ui/page`. The composed application is asked the same questions by
[apps/platformkit](../apps/platformkit): whether the list it composes loads what its live
region runs on, and whether one tenant's notices reach nobody at another
(`TestTheComposedNoticeListLoadsWhatItsLiveRegionRunsOn`,
`TestTheNoticeListShowsOneTenantsNoticesToNobodyAtAnother`). What no test here observes is a
browser: the rate one region refreshes at, and the refresh that follows the last frame of a
burst, are the controller's own behaviour, and no journey drives them yet.
