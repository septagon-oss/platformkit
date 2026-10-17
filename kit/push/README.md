# kit/push

The transport below a notification module's provider: one rendered message, one
device, one answer. `kit/push` owns none of the questions around it — who is
told, whether they asked to be, what the words say, which ledger row records the
attempt — because the notification module answers those and translation owns the
words. What it owns is the part that needs a push service: the routing, the
protocol, and the difference between a device that is gone and a service that is
having a bad afternoon.

```go
adapters := push.Adapters{
	push.PlatformWeb:  webpush.New(webpush.Config{PublicKey: pub, PrivateKey: priv, Subscriber: "mailto:push@example.test"}),
	push.PlatformExpo: expo.New(expo.Config{}),
}
receipt, err := adapters.Send(ctx, device, message)
```

## The port

| name | what it is |
|---|---|
| `Platform` | the carrier family a device speaks: `web`, `expo`, and `fcm`/`apns` named for the day an adapter lands |
| `Device` | one token, what speaks it, and the tenant and person it belongs to |
| `Message` | a rendered notice: title, body, deep link, collapse tag, TTL |
| `Adapter` | `Platform`, `Validate`, `Send` |
| `Adapters` | the composition's answer to "what does this installation carry" |
| `Receipt` | `delivered`, or `accepted` with the provider's id for the send |

`Adapters.Send` is the one send and the one span. It routes on the device's
platform and opens `push send` with the platform, the tenant, the device and the
outcome; the adapter's own HTTP call is a child of it. A caller that takes an
adapter out of the map owes itself a span.

## What an error costs a device

The vocabulary is small because the consequence is a row in a table.

| error | what it means | what the caller holding the row does |
|---|---|---|
| `ErrGone` | the provider no longer knows this token | deletes the row; a gone token that is retried never goes away |
| `ErrMalformed` | the stored token is not a syntax this adapter could carry | keeps it; the person's client re-registers |
| `ErrRejected` | the provider refused the message as written | keeps it; nothing about the device is wrong |
| `ErrInvalid` | our own check refused before anything was sent | fixes the call |
| `ErrUnsupportedPlatform` | no adapter behind the device's platform | fixes the deployment |
| anything else | nothing at all about the device | retries; the row is untouched |

The mapping is the design, and `pushtest` holds it: a `429`, a `5xx` or a dropped
connection is never `ErrGone`, because an adapter that mapped "not a 2xx" to a
gone token would delete the devices of everybody in a bad hour. `Gone(reason)`
carries the provider's own sentence for an operator to read; it never carries the
token.

## Sizes and clocks

`MaxBody` is 4096 bytes: every transport here reaches a limit near it, and a
notice longer than the limit is clipped at a rune boundary rather than refused —
the alternative is that somebody is not told at all. The send says it clipped
(`push.truncated`). Web Push is the tighter of the two: the whole encrypted
record, envelope and copy together, must fit one record, so its payload is
shorter than the port's ceiling. `MaxTTL` is one day; a TTL of zero means that
rather than the platform's own default, which for Expo is four weeks and for both
platforms means a notice that arrives next month.

## Adapters

| adapter | what it speaks | what a token is |
|---|---|---|
| `providers/log` | a log line, for development and for a test | any token, refused only if empty, oversized or containing a control character |
| `providers/webpush` | Web Push with VAPID (RFC 8030, RFC 8291, RFC 8292) | the browser's serialised `PushSubscription` JSON, endpoint required to be https |
| `providers/expo` | Expo's push HTTP API, which reaches FCM and APNs | an Expo push token, checked as an opaque word and never by spelling |

The log adapter stands behind every platform at once, because it is the
composition's stand-in and a development machine has to carry whatever a client
registers. It never reports a token gone — a test that wants that path wires a
stand-in that answers it.

`providers/expo` is `net/http` and `encoding/json` and no SDK: the API is one
POST, and a dependency would buy a name a reader has to look up to understand it.
`providers/webpush` does import one (`github.com/SherClockHolmes/webpush-go`,
MIT, attributed in `NOTICE`), because RFC 8291's encryption and RFC 8292's JWT
are not something to rewrite here.

Both talk to whatever URL the device's token holds or the configuration names, so
their suites drive them against `httptest` stand-ins and no test reaches the
network.

## The conformance suite

`pushtest` is the suite every adapter owes, written in `kit/porttest`. A provider's
test supplies a `Harness` — the tokens its platform speaks, the states its
stand-in can be put into, and the world — and gets the success, the gone token,
the malformed token, the unavailable service, the refusal, the one-span-per-send,
the token-in-no-attribute and the clipped-body cases. Those three last ones are
shared rather than copied: a trace that holds a device token is a keystore, and an
adapter is not free to keep the contract differently.

## Limits

* No batching. One `Send` is one device and one request; Expo's 100-message
  batching wants a port method that takes many devices, which would move a
  gone-token removal across a request boundary.
* No receipt polling. Expo's `DeviceNotRegistered` arrives in the ticket
  sometimes and in the receipt usually, up to 24 hours later; polling needs a
  scheduler and an outcome the delivery ledger does not have.
* No device store, no preferences, no ledger, no copy.
* Nothing carries a client name, a tenant or a price. The VAPID pair, the
  `Subscriber` contact and an Expo access token are configuration the
  composition passes in.
