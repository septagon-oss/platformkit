# Boolean feature evaluation

`Evaluator` owns the application contract for optional product behavior.
Permissions, tenant isolation and subscription entitlements still apply when a
flag evaluates to true. This package does not administer flags or deploy flagd.

Compose `NewOFREP` with an application, installation and environment scope,
an existing service URL, an explicit timeout and optional bearer token. TLS
verifies the server against system roots or the supplied CA file. HTTP requires
an explicitly configured loopback endpoint for local development. Construction
validates configuration without a network request; the first evaluation checks
reachability. Drain requests before calling `Close`, which releases the owned
HTTP transport's idle connections and the isolated SDK instance.

Pass each module the `Evaluator` interface. Before an operation, call `Boolean`
with the resolved tenant, a stable trusted subject identifier and an explicit
fallback. Errors return that fallback with `Defaulted: true`; a configured false
value is a successful decision. Missing flags, wrong types and provider outages
have distinct errors. Provider diagnostic text does not escape the adapter.

The provider receives `application`, `installation`, `environment`, `tenant_id`
and `subject` attributes. Its targeting key is the JSON array of those five
values, preventing different tenants or installations sharing rollout buckets.
These attributes are evaluation facts, not an authorization boundary: deployment
must restrict access to the flag service. Use pseudonymous subject identifiers.

OFREP keys contain ASCII letters, digits, dots, underscores or hyphens; `.` and
`..` alone are refused. This prevents path traversal through the upstream URL
join. Redirects are refused, and ambient proxy or `FLAGD_*` variables do not
configure this adapter. Targeting and evaluation belong to the provider; there
is no PlatformKit rule engine, cache or event stream.

[Flagd supports OFREP](https://flagd.dev/reference/flagd-ofrep/) on port 8016 by
default and currently labels that service experimental. An installation must
configure and verify its endpoint separately. `NewOpenFeature` also accepts
other fresh providers at the composition boundary. Those providers must honor
evaluation contexts; arbitrary blocking provider code cannot be preempted.

Run `go test -race ./kit/flags` for scope separation, concurrent evaluation,
fallback, cancellation, lifecycle, TLS wire requests and configuration refusal.
These use local fixtures; deployed service readiness, administration UI and
downstream application adoption require separate verification.
