# Memory mailbox

`New()` creates a `Mailbox` implementing the [mail contract](../../README.md).
`Send` retains each message in memory and reports success, including with a
canceled context. It sends no network request. This is an explicit choice for
tests or unconfigured delivery, not proof that a recipient received mail.

`Sent` returns a detached snapshot in recorded order. Concurrent sends and
snapshots are supported. Recipient and subject are logged for diagnostics;
bodies, which may contain account links, are never logged. Stored messages live
until the mailbox is discarded, are not bounded by this provider and disappear
when the process ends. Keep the mailbox private to its composing application.

From the foundation root, run `go test -race ./kit/mail/providers/memory` for
concurrent retention, snapshot independence and body-free logging. The provider
imports only stdlib and `kit/mail`, without a test harness in production code.
