# Storage provider conformance

Call `RunStorage(t, fresh)` with a factory returning a new `StorageFixture` for
each call. The factory owns cleanup and must isolate repeated scopes, including
those constructed by one subtest. Set `RequiresSize` only when the provider
deliberately refuses unknown-length uploads.

The suite checks streamed bytes, empty/unknown-length input, create collisions,
concurrent winners, missing reads, retry-safe deletion, failed readers, scope
isolation and optional listing cutoffs. It imports `testing`, UUID generation
and the [blob contract](../README.md), with no database or File service.

The [local provider test](../providers/local/local_test.go) shows construction.
Run it with `go test -race ./kit/blob/providers/local` from the foundation root.
Passing this suite does not establish provider-specific cancellation, multipart
atomicity, range behavior or a particular deployed server version; qualify
those behaviors where the provider claims them.
