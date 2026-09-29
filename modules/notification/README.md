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
