# Kernel measurement vocabulary

`kit/telemetry` holds the vocabulary a kernel package needs to measure anything —
the attribute keys, the request id and how it travels, the three instruments this
runtime promises, and the closed set of refusal classes — and no provider, no
exporter and no sampler. Those are chosen once, by the composition, in `kit/app`,
which is the only package in this repository whose dependency closure may hold an
OpenTelemetry SDK. `scripts/check_packages.sh` refuses the day that stops being
true, in two moves: the closure bound it measures speaks for the packages that
script is asked about, and its last line speaks for the whole tree, refusing an
import of `go.opentelemetry.io/otel/sdk…` or `…/exporters…` in any non-test Go file
outside `kit/app/`, the rule `TestOnlyTheCompositionLinksTheMeasurementSDK`
asserts from inside the suite. `kit/httpx`, `kit/db`, `kit/events` and `kit/jobs`
import this package and the OpenTelemetry *API* beside it.

## Reused / Added / Made reusable

**Reused.** The `X-Request-ID` mechanism `kit/httpx` already had, `log/slog`,
`kit/problem`'s one error shape, `health.Check` (which gained a sibling type
rather than a fork), the T-0018 migration runner, `dbtest.Schema`, the memory
transport and the JetStream conformance fixtures, `otelhttp` as the W3C-reading
server span, and the SDK's own `tracetest.NewSpanRecorder` and
`sdkmetric.NewManualReader` as the test doubles. No second correlation, error,
health or migration mechanism, and no rebuilt double.

**Added.** The four attribute keys and `SpanAttrs`; `MetricAttrs`, the same two
tenant keys with the request id left off, for the places that record a number rather
than open a span; `WithRequestID`/`RequestID`, which carry the id as W3C Baggage so a
correlation value that is not a trace parent rides the standard carrier;
`Propagators()`, which every PlatformKit process installs whether or not it exports;
the three instruments of `Instruments` and their nil-safe recorders; and
`RefusalClass`, the thirteen-class closed set a refusal counter keys on.

**Made reusable.** `health.Report` (a reading an operator sees that cannot move a
verdict, which is what the type was extracted for), `telemetry.Tracer()` for any
span anybody opens — a call and not a package variable, so a span arrives at the
provider the process installed and not the first one it ever installed, which is
what lets `kit/db`, `kit/jobs` and `kit/events` open their spans without holding a
tracer, a provider or a field for either (`kit/httpx` is traced by `otelhttp` and
stamps its span through the context, so it imports this package and does not call
that one),
`telemetry.Propagators()` for a process that composes itself without `kit/app`,
`telemetry.SpanAttrs` for whatever span anybody opens, `telemetry.MetricAttrs` for a
number that has to say whose it is without saying which request, and
`telemetry.Shared` for a number recorded from a package that has no provider of its
own to be handed.

## The two tenant keys, and why the tenant is never a resource attribute

`pkit.tenant` is the slug and `pkit.tenant.id` the UUID: a delivery has only the
id its event names and a request has both, so one key carrying two different kinds
of value would be unfilterable by anybody who does not already know which span they
are on. A key is written only where its value was learned — a host the resolver does
not know has neither.

A resource describes the *process*, and this process serves many tenants (0028's
shared-instance mode resolves one per request, from the host), so a tenant in the
resource would be a lie for every request but one, or one provider per tenant, which
is the unpickable singleton the pillar contract refuses. `pkit.client` is the one
exception and is a deployment's own fact, not a request's.

## What is deliberately absent

No collector, no dashboard, no actor on a span, no `messaging.system` (which broker
carried a message is the adapter's fact and this package does not know it), no
`sample_ratio` read from the environment, and no `pkit.client` invented for a shared
installation. `kit/app` decides all of those, from `config.Telemetry`.

Three of the four traced boundaries carry a *number*; the fourth carries only its span.
`pkit.http.operation.duration` and `pkit.http.refusals` are recorded by `kit/httpx` for a registered
operation (`traced.go`), `pkit.outbox.lag` by the relay in `kit/events` (`relay.go`), and a job's or
a delivery's latency is asked of its span, because `kit/jobs` and `kit/events` open spans and record
no duration. The `pkit.http.` prefix is therefore a statement of reach and not of the kind: the
histogram holds HTTP operations today, and giving the job boundary a duration number is a fourth
instrument with its own name, not a broader use of this one.

## Limits

Two targets the brief names have no target in this repository, and the deviation belongs
here rather than in a reader's surprise: no `deploy/chart` values declare a collector,
because this repository has never had a chart — the collector is the `telemetry` profile of
[`compose.yaml`](../../compose.yaml) (`deploy/otel-collector.yaml`, brought up by `make
trace`), which is where this tree deploys one thing.

The second is a tool this repository does not own: `tools/pillars.py` is the programme's,
not the kernel's (`ls tools/` → `designexport locbudget`). Its absence leaves the brief's
"measurably better" line no less answerable, because the indicator that tool computes for
this pillar is a reading of one file — *OpenTelemetry in the kernel*, whether `go.mod` names
`go.opentelemetry.io/otel` — and one command gives its value at both ends. `0c3a040` is the
`origin/main` commit this branch sits on, which `git merge-base HEAD origin/main` prints and
which every merge moves, so a merge restates it:

```sh
grep -c 'go.opentelemetry.io/otel' go.mod                     # 8
git show 0c3a040:go.mod | grep -c 'go.opentelemetry.io/otel'  # 0
```

True here, false there. The branch's own `build(deps)` commit is the change that adds the
dependency, and the tool reads that same boolean off `origin/main`, so the tool's reading of
it turns true the day this merges.

The ratio this delivery reports beside it — 80 of 81 boundaries carrying a tenant — was
measured against the reference application's own composition and is not recomputable from
this tree: no committed symbol or tool counts the 81, which is the product's share. What is
committed is the numerator's evidence, one case per kind of boundary that reads the tenant
dimension back off the span, metric or row the boundary emits.
