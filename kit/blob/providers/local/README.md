# Local blob storage

Call `New(directory)` to construct a `*Local` implementing
[blob.Storage and Lister](../../README.md). Construction touches no disk. The
first `Put` creates private directories and a create-only file under the key's
two-character shard. Keys must be lowercase, hyphenated UUIDs, as in File's
existing layout; uppercase or path-shaped keys are rejected.

The provider streams bytes and accepts unknown length (`-1`). A collision keeps
the existing bytes. Failed writes attempt to remove their partial file; an
error removing it can leave an orphan. Missing reads return `blob.ErrNoBlob`;
other read failures retain their error. Returned files support seeking and
belong to the caller to close. Repeated deletion of a missing key succeeds.

`Keys` uses modification time and a strict before-cutoff comparison within the
configured root. File's separate job chooses the safety interval and checks
metadata references before deletion. This provider does no authorization or
reconciliation by itself. The directory must be application-owned; it is not
an isolation boundary against another process that can modify its contents.

Operations preserve the existing local implementation's synchronous behavior:
they accept context for the shared interface but do not interrupt filesystem
I/O or a blocked input reader on cancellation. A successful `Put` closes the
file; it does not promise an fsync durability boundary.

From the foundation root, run `go test -race ./kit/blob/providers/local` for
streaming, collisions, concurrency, listing, failures and seeking. This package
imports only the standard library and `kit/blob`.
