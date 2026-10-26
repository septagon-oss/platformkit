# One ask, one model, one allowance

## Purpose

`kit/inference` asks a model one question about text and at most one picture. It
is a port and not a module: no entity, no table, no route, no permission, no
manifest — the counter that rations its calls and the tracer that measures them
are reached by construction, through ports a composition fills. Decision 0087 §4
named the seam; 0093 narrowed it to *chat, at most one image, returning text*, and
took embeddings, tools, streaming and provider routing out of it. This package
will not take them back on a caller's convenience: chat parity does not imply
embeddings parity, and a port holding both makes every adapter answer for a wire
nobody measured.

## Public surface

The public ports are `Engine`, `Provider` and `Counter`; `Request`, `Answer`,
`Response` and `Usage` carry one call. `New` builds a capped engine and `Practice`
builds the deterministic default over `Script`. `inferencetest.Conformance` is
the shared adapter suite; `providers/openai.New` supplies the HTTP adapter.
There are no entities, mutation commands, queries, events or permission keys.

### Interface

`New` requires a positive `DailyCalls` and a `Counter`. `Practice` constructs
an uncapped engine over the shipped `Script`, with no external wire.

`Chat` checks cancellation, the ask and the tenant, spends the allowance, dials, then
judges the answer. Refused calls are refused before the counter is touched — an ask
that was never allowed spends no allowance — and the allowance is spent *before* the
wire, so an exhausted tenant cannot run a bill by knocking. A call that reached the
endpoint stays counted when its answer comes back too large, truncated, empty or lacking token usage: the
provider spent its tokens, and un-counting it would make a chatty endpoint free to
hammer. There is no refund and no idempotency key; a chat call is a spend, not a
set, and a caller that retries costs itself another unit.

## Compose it

The application supplies the model, endpoint and secret to the OpenAI-compatible
adapter, and constructs `New` with a tenant-scoped `Counter` and a positive
`DailyCalls`. `limit.Postgres` or `limit.PostgresOf` supplies that counter.
The owning command checks the actor's grants in its tenant transaction; it calls
`Chat` after that transaction closes. No application composes this port here.

**Reused**: `kit/limit`'s row, its fixed window and its four failure worlds
([ADR 0010](../../docs/adr/0010-a-limit-is-a-row.md)), asked of through a
consumer-declared `Counter` in the shape `kit/httpx` names `WriteLimiter`, which is
why this package's closure holds no `database/sql`; `kit/telemetry`'s `Tracer`,
`Scope` and `SpanAttrs`, and the GenAI keys read from the pinned
`go.opentelemetry.io/otel/semconv/v1.38.0` rather than hand-spelled; `kit/fault`'s
`ErrInvalid` under a refused ask; `kit/cache` + `kit/cache/cachetest` as the shape
of a kernel part with one conformance suite its adapters run; `kit/flags/providers/ofrep`'s
URL, secret and no-redirect client as the adapter's refusal set.
**Added**: the ask, the answer, the three byte bounds, the tenant's daily call
allowance and its four worlds, and the rule that an answer this kit cannot prove is
whole is not returned at all. **Made reusable**: the port, the script a consumer's
test asserts on (`Script.Asks`), and the suite every adapter runs.

## What it owns and does not own

The engine owns request and answer bounds, tenant allowance checks and safe
telemetry. `kit/limit` owns the counter row, its fixed window and concurrency;
`kit/telemetry` supplies tracing. The product owns actor authorization, copy,
chunking, retries and any additional per-person ration. There is no retention
or export store here, and no page or native screen.

### Nothing about the ask leaves the process

No prompt, instruction, image byte or answer text reaches a span attribute, an event, a
log line or an error string, and the span carries **zero events**: an exception message
is a sentence a later wrapper is free to rewrite with the caller's text inside it, and
"no event on this span" is a promise that survives other people's edits where "careful
messages" does not. The adapter quotes no response body for the same reason — a vendor's
error page can echo the prompt it was sent — and carries no trace context outbound,
because it is a plain `http.Client` with no instrumentation and its own case reads the
request shape off the wire. What a trace backend holds is the model, the provider, the
duration and the token counts the provider itself reported. Missing or negative
usage returns `ErrAnswerShape` and no answer. Response model metadata enters a span
only when it equals the configured model; arbitrary finish reasons are never quoted.

## Invariants, refusals and concurrency

The Postgres counter serializes spending in its own transaction; an allowance
refusal makes no provider call. There is no entity revision or idempotency promise.
A call with an unknown provider outcome stays counted, as do refused answers
after the wire. The owning product decides whether another attempt is worth
another unit.

| refusal | class | correctable | by whom, and how |
| --- | --- | --- | --- |
| `ErrSpent` | `allowance_spent` | by the operator | raise `DailyCalls`; the caller may retry after `Retry-After`. This is the brief's "refuses by name, never silently", and `ErrSpentUnproven` beside it is the same refusal when the counter's row was somebody else's, when nothing about the window is known |
| `ErrCounterUnavailable` | `counter_unavailable` | by nobody at runtime | **the allowance fails closed**: an uncounted model call is a bill nobody authorised, which is the judgment `kit/httpx/access.go` makes of its own access counter and the opposite of what `modules/auth`'s lockout chooses. Row four of `kit/limit/README.md` |
| `ErrNoTenant` | `no_tenant` | by nobody at runtime | the caller resolved no tenant. One shared bucket would let one customer spend the installation's allowance, so this port refuses rather than counting under the nil UUID — a job or a delivery resolves its tenant before it asks |
| `ErrAnswerIncomplete`, `ErrAnswerEmpty`, `ErrAnswerTooLarge` | `answer_*` | by the caller or the operator | the answer is not returned half. `Finish` is read here rather than left to callers because a truncated guess is the failure no caller can detect afterwards, and the call stays counted either way |

Everything else — `ErrAskShape`, `ErrAskTooLarge`, `ErrImageTooLarge`,
`ErrImageFormat`, `ErrAnswerShape`, `ErrResponseTooLarge`, `ErrProviderRefused`,
`ErrProviderUnavailable`, `ErrClosed`, `ErrNoScript` — is named in the source with who
can fix it, and `StatusFor` answers the HTTP verdict a door should give each.

## Acceptance

| observable | maintained evidence |
| --- | --- |
| script and HTTP adapter honour deadlines, cancellation, answer bounds and allowance exhaustion | `TestEveryAdapterRunsTheOneSuite` in [inference_test.go](inference_test.go), over [inferencetest](inferencetest/conformance.go) |
| prompts and pictures stay out of diagnostics | `TestNoPictureReachesATraceAttributeEventOrError` in [inference_test.go](inference_test.go) |
| concurrent calls cannot spend the last unit twice; tenants keep separate allowances; a held row refuses without a provider call | [postgres_allowance_test.go](postgres_allowance_test.go) |
| compressed responses are bounded after decompression | `TestACompressedResponseIsBoundedByWhatItExpandsTo` in [compressed_response_bound_test.go](providers/openai/compressed_response_bound_test.go) |

These are kernel-port checks; no product-tier, browser or device journey is
claimed. No migration or application composition is added.

## Limits

This port has no permission or actor policy: the owning command authorizes the ask
in its tenant transaction before calling the engine outside that transaction.
Streaming, embeddings, tools, provider routing and retries are outside its contract.
Machine translation through `locale.Translator` is an intended second consumer;
the composition bridge and real vendor compatibility remain unverified here.
The maintained tests include real Postgres tenant isolation, exhausted allowance,
concurrent last-unit spending and held-row refusal through `dbtest.Schema`/`Hold`.
They are package tests run by `make check`; the reusable conformance legs currently
use memory counters rather than a real-store leg.

## Verify

### Verification

`make check` runs the suite from `kit/inference` over both far ends this repository
holds: the script, and the OpenAI-compatible adapter over a loopback stub. That is the
whole of the portability claim this checkout can make on its own.

```
make inference-conformance PROVIDERS=ollama,hosted
```

One leg per name, configured by `PLATFORMKIT_TEST_INFERENCE_<NAME>_URL`, `_MODEL`
and `_KEY`. A named provider declares that it Honours the port-semantics tier only —
a hosted vendor will not produce a 37-byte truncated answer on cue — so the suite
skips the far-end cases and prints why, beside the number of asks the leg cost. The
leg **fails** if the endpoint cannot be reached, and prints no key: the configuration
is read, named by variable and never echoed. A PR that runs it names the providers in
its `Verified:`, or says under `Not verified:` which one it could not reach.

Loopback HTTP endpoints are enabled by the provider runner; other endpoints require
HTTPS. The adapter still refuses redirects and plain HTTP off loopback.

The full check needs the development services documented in
[CONTRIBUTING.md](../../CONTRIBUTING.md#verify-at-the-relevant-boundary).
Live Ollama and hosted-provider compatibility, browser/native journeys and CI
were not verified by this delivery.

## Why the port stops where it does

The two kernel parts this port is built on — the counter that rations calls and the
tracer that measures them — are reached by construction, as ports the composition
fills, rather than by composing a module. That is why there is no manifest: an
engine that could find its own limiter would be a port that decides who may call.

Streaming, embeddings, tool calls and provider routing are absent by the same
argument. A provider that streams is a different interaction, and a provider that
does embeddings is a different wire: parity on chat does not imply parity there, so
each of those arrives as a second port with its own conformance suite rather than
as a method this one's adapters must all implement. A caller that needs a long
document read chunks it and asks more than once; the byte bounds are what make that
loop safe.

Nothing may be asked in a caller's name: no permission is consulted, because the
three checks that decide whether a call happens are mechanical — an allowance
composed for the caller, a bounded input, and no transaction open around the ask.
This is the inference seam of decisions 0087 and 0093; machine translation through
`locale.Translator` is its intended second consumer.
