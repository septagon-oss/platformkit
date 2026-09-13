# Byte storage

`Storage` owns streamed bytes through create-only `Put`, `Get` and retry-safe
`Delete`. `Lister` optionally enumerates keys older than a supplied cutoff.
`ErrNoBlob` distinguishes a missing value from an unavailable provider. The
contract imports only the standard library and starts no service.

Select a provider explicitly, such as [local filesystem storage](providers/local/README.md),
and pass the interface to its consumer. The caller closes every returned reader
and controls metadata, authorization and the ordering of effects. A byte write
cannot be rolled back by a database transaction. Providers document permitted
keys, unknown-length uploads, cancellation and optional seek support.

File retains its upload metadata, tenant quotas, visibility, authorized streaming
and orphan job. Its existing Storage/Lister names alias these contracts;
`file.Local` forwards to the same local implementation. No File schema or data
migration accompanies this extraction. Object-store adapters remain separately
owned; this package does not select or configure one.

Run `go test -race ./kit/blob/...` from the foundation root. New provider tests
use [blobtest](blobtest/README.md) directly to avoid importing File's database
conformance. `go list -deps ./kit/blob` must contain no external or runtime
packages beyond this contract itself.
