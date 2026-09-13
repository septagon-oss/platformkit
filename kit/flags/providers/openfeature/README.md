# OpenFeature adapter

`New(ctx, scope, provider, timeout)` connects a fresh OpenFeature SDK provider to
the [PlatformKit flag contract](../../README.md). It returns `*Evaluator`, whose
`Boolean` method uses the owned flag values and whose `Close` method releases the
isolated SDK instance. Import this package only when selecting an SDK provider.

Supply `flags.Scope` from trusted application composition. Invalid scope,
nil provider or a nonpositive timeout refuses construction before ownership
transfers. Once initialization begins, this adapter owns the provider, including
cleanup after initialization fails. Initialization and evaluations use bounded
contexts; a custom provider must honor them. Arbitrarily blocking provider code
cannot be forcibly stopped.

Each evaluator owns its SDK API. Tenant and subject values form a JSON tuple
with application, installation and environment, preserving distinct rollout
identities without delimiter collisions. SDK diagnostics are mapped to the
contract's errors; failures return the caller's fallback with `Defaulted` set.
Successful false values remain distinguishable from failure.

Drain application requests before calling `Close(ctx)`. It shuts the provider
down once, with a bounded context; later evaluations return `flags.ErrClosed`.
The application retains ownership of its domain permissions and entitlements.

Run `go test -race ./kit/flags/providers/openfeature` from the foundation root
for isolated concurrent targeting, fallback, initialization failure,
cancellation and cleanup. These fixtures qualify the adapter, not every SDK
provider or a deployed flag service. [Constructor migration](../../README.md#migrate-constructor-imports)
describes the former root-package API.
