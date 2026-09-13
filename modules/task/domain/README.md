# Task resolution decisions

Import `github.com/septagon-oss/platformkit/modules/task/domain` from an ordinary
Go application to decide task resolutions without PlatformKit's runtime. This
package uses only the standard library. It shares the existing task lifecycle
values; [contracts](../contracts/task.go) retains compatible public constants.

Call `Resolve(status, recorded, requested)` with current task facts. Inspect the
error first, then `Changed`: empty text is a valid new resolution. A false
`Changed` leaves the original record and timestamp untouched. The executable
[example](resolve_test.go) shows the API.

The caller owns authorization, current-state synchronization, timestamps and
persistence. A decision is not a committed event. The existing
[SQL service](../internal/service.go) locks and authorizes first, then applies
the decision and publishes through the caller's transaction. Its
[fake](../contracts/tasktest/fake.go) shares the rule but does not model policy,
transactions or rollback. Cross-module PlatformKit integrations continue to
use the existing service contracts for those guarantees.

Run these local, service-free checks from the foundation repository:

```sh
go test ./modules/task/domain ./modules/task/contracts/tasktest
go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./modules/task/domain
```

Tests must pass; the dependency listing must contain only this domain package.
Database, authorization and outbox checks run through the repository's
[contribution workflow](../../../CONTRIBUTING.md#verify-at-the-relevant-boundary).
Assignment and SLA decisions remain in their current owners. This extraction
does not yet provide an independently persisted Task service or a frontend.
