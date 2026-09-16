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
