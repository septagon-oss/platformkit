# Notification module

`modules/notification` tells somebody something, in the application and
optionally by mail. Rows are addressed to a person, so the two routes under
`/api/v1/notification/notifications` are scoped by the principal rather than by
a permission, and the manifest declares neither permissions nor a navigation
entry; a subscription delivers the mail-marked rows through the composed
`Mailer`. Without a mail host the rows are still recorded and the mails are
logged, which is what a development machine wants.

Compose it with `notification.Deps{Recipients, Senders, Providers, Mailer, Hosts,
Secure}`: `SMTP` is the production mailer for `config.example.yaml`'s `mail` section
(password through `PLATFORMKIT_MAIL_PASSWORD`), `NewMailbox()` the in-memory one, and
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

## Authorization

### Permissions

None. `permissions` in `modules/notification/module.go` is an empty `[]module.Permission`, and the module has no nav entry.
Both routes are guarded by `httpx.SignedIn()` in `modules/notification/internal/handler.go`: `GET /notifications` (`notification-notification-list`) and `POST /notifications/{id}/read` (`notification-notification-read`).
The `SendMail` event subscription (`modules/notification/internal/mail.go`) is run by the kernel and is not guarded by a permission.

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

None. There are no permissions, so none is marked `Operator: true`, and no `OperatorRead` or `OperatorWrite` route exists.

### Provisioning

Nothing needs granting. Every signed-in person can read and mark their own notifications, which is why the manifest declares no key.
The settings and sender-admin faces (`Settings()`, `Senders(verifier, keys)`) are reached by no route yet: the module holds the commands, and the routes and the permission a role would be granted for them are the application's share.
Roles and the roles API therefore do not affect this module's routes.
