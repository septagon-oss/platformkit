# Outgoing mail

`Message` is rendered recipient, subject and body text. `Mailer.Send` performs
delivery through an explicitly selected provider. This contract imports only
the standard library; it owns no recipient directory, template engine, queue,
database or notification module.

Select [SMTP](providers/smtp/README.md) for delivery or the
[memory mailbox](providers/memory/README.md) for explicitly retained, unsent
messages. Pass only the `Mailer` contract to consumers. The caller owns retry
policy and interprets uncertain delivery: an error can follow server acceptance,
and a database rollback cannot unsend a message.

Auth and Notification retain their existing Message/Mailer aliases. Auth's
credential-bearing bodies stay out of ordinary notification rows, events and
logs; Notification still owns its normal notice/outbox workflow. Existing
`notification.SMTP`, `notification.Mail` and `NewMailbox` continue to compose
the same providers. This extraction changes no templates or stored data.

Run `go test -race ./kit/mail/...` from the foundation root for provider cases.
`go list -deps ./kit/mail` should contain only this contract and standard-library
packages. Provider tests do not establish live relay delivery or the complete
notification/outbox journey.
