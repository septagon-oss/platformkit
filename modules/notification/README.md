# Notification module

`modules/notification` tells somebody something, in the application and
optionally by mail. Rows are addressed to a person, so the two routes under
`/api/v1/notification/notifications` are scoped by the principal rather than by
a permission, and the manifest declares neither permissions nor a navigation
entry; a subscription delivers the mail-marked rows through the composed
`Mailer`. Without a mail host the rows are still recorded and the mails are
logged, which is what a development machine wants.

Compose it with `notification.Deps{Recipients, Mailer, Hosts, Secure}`: `SMTP`
is the production mailer for `config.example.yaml`'s `mail` section (password
through `PLATFORMKIT_MAIL_PASSWORD`) and `NewMailbox()` the in-memory one.
Consumers import [contracts/](contracts/) and its
[fake](contracts/notificationtest/), never `internal/`; mail templates live in
[internal/templates](internal/templates/).

Every channel a notice asks for is accounted for in `notification_deliveries`
(migration 000027), under the tenant's row-level security and append-only: a
`requested` row when the notice is written, then a terminal row in the same
transaction as the step that finishes the channel — `sent`, or `suppressed` with
the reason (no address, the notice deleted before its mail was due). In-app is
requested and sent with the row itself. A relay that refuses leaves no terminal
row, because its transaction rolls back and the outbox retries; a send that can
never succeed is recorded by the kernel in `platformkit_dead_letters`.
`delivery_ledger_coverage` — requested channels with a terminal row, over all
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
asked. It holds no subject, body, link or credential.
**`delivery_ledger_coverage` does not count these rows and never will**: the
register's number is requested channels of notices with a terminal row, and a
mail with no notice and no `requested` half would silently narrow what that name
means. Ask the two tables two questions.

## Authorization

### Permissions

None. `permissions` in `modules/notification/module.go` is an empty `[]module.Permission`, and the module has no nav entry.
Both routes are guarded by `httpx.SignedIn()` in `modules/notification/internal/handler.go`: `GET /notifications` (`notification-notification-list`) and `POST /notifications/{id}/read` (`notification-notification-read`).
The `SendMail` event subscription (`modules/notification/internal/mail.go`) is run by the kernel and is not guarded by a permission.

### Object scope

None. No call to `tenancy.Policy` and no `Resource.Kind` appears in the module.
Scope is the caller: `caller` reads the principal's `UserID` from `tenancy.PrincipalFrom`, never from a parameter, and `ListFor` and `MarkRead` take that id.
`MailLedger` is written by the module that sent the mail, inside that module's own tenant transaction, and read only by the request id of the caller's own call; `direct_mail_deliveries` has no route and no permission of its own.

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
Roles and the roles API therefore do not affect this module's routes.
