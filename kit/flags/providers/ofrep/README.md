# OFREP adapter

`New(ctx, Config)` connects an explicitly selected OFREP endpoint to the
[flag contract](../../README.md). It returns the
[OpenFeature adapter's evaluator](../openfeature/README.md), with `Boolean` and
`Close`. There is one implementation of targeting, fallback and SDK lifecycle.

Supply scope, URL, timeout and optional CA file/bearer token through application
composition. Construction reads a supplied CA file and validates configuration;
it performs no service request. Evaluation is the first reachability/trust
check. HTTPS verifies the server using system roots or the supplied CA.
`Insecure` permits HTTP only at an explicitly configured loopback endpoint.
Ambient proxies and provider environment variables do not select this service.

Redirects and ambiguous URL components are refused. Flag keys contain ASCII
letters, digits, dots, underscores or hyphens, excluding `.` and `..`; other
keys are refused before a request. This retains a single unambiguous path
segment in the upstream SDK. Errors return sanitized contract outcomes and the
caller's fallback. Credentials and provider diagnostics must remain private.

Drain requests before `Close(ctx)`, which closes idle transport connections and
the isolated SDK instance. This adapter reads evaluations; it does not publish
flag definitions, synchronize configuration or administer a server.

From the foundation root, run `go test -race ./kit/flags/providers/ofrep` for TLS
requests, scope attributes, error mapping, cancellation, redirects, unsafe keys,
loopback selection and cleanup. Those local fixtures do not establish live
server compatibility or rollout administration.
