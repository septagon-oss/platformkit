# SMTP delivery

`New(Mail{Host, Port, Username, Password, From})` creates a sender implementing
the [mail contract](../../README.md), without connecting. `Send` opens one
connection per message and closes it afterwards. The application owns the
configuration and must keep credentials private.

The provider uses stdlib SMTP with STARTTLS when advertised. A failed upgrade
refuses delivery; absence of STARTTLS retains the existing plaintext-relay
behavior. Authentication runs only when a username is configured. Subjects are
folded into one header line; bodies are sent as UTF-8 plain text with CRLF
line endings. No retry loop or persistent queue is created here.

Context cancellation controls dialing. Once connected, each read/write has a
refreshed 30-second deadline; the context does not interrupt that conversation.
This bounds a stalled step, not the total duration of a continuously progressing
message. A failed return can still follow acceptance, so callers must preserve
their existing uncertain-delivery and retry policy.

Run `go test -race ./kit/mail/providers/smtp` from the foundation root for a
stalled local server and wire formatting. Notification's subscription failure,
outbox and credential handling remain separate integration tests. This package
adds only stdlib and `kit/mail` dependencies; no live SMTP server is qualified
by this extraction.
