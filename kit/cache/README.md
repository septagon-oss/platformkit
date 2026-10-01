# One value every replica reads

A map in one process's memory is three answers when three pods are running, and none
after a deploy. It is also three answers to "who may I treat this host as?": a
suspension that invalidates one process leaves the other two believing what the
installation just stopped believing, for the whole life of the entry. This package is
the port for a value a replica may recompute — the tenant inside the key type, and an
invalidation that closes what was already written.

**Reused**: `kit/limit`'s shape — a port over a shared store, an in-process adapter
that is a deployment for one process and the double every test runs on at once, a
statement budget on a detached context, and a fixed-width tenant prefix so a name
cannot shift an earlier segment of a key. `kit/events/providers/<name>` for where a
store lives, and `kit/db/dbtest` as the precedent for a kernel package's harness.

**Added**: the two things no shared map gives you. A `Key` that cannot be built
without saying whose value it is (`Of` for a tenant's, `Shared` for the
installation's), and `Move`, which closes a generation: every entry carries the
generation it was written under, so a write that read the counter before an
invalidation and lands after it is not believed. `Group[V]` is decision 0028 §4's
bounded lazy composition on the same port.

**Made reusable**: `Backend`, four commands (`GetMany`, `Set`, `Delete`, `Raise`).
The generation, the envelope, the budget, the batching and the refusal of an entry
with no lifetime belong to this package and not to an adapter, so a second store is
four methods and cannot get the invalidation wrong. `cachetest` is the one conformance
suite both adapters run.

Sessions, permission grants and entitlements are never cached here, in this process or
in any store: `modules/auth`'s own kernel says a permission cache is a window in which
a revoked grant still works. The read path for those is the transaction under
row-level security. [docs/cache.md](../../docs/cache.md) holds the table.
