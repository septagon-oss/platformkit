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
generation it was written under, and `Get` hands back the generation its own read
found open so that `Set` can be stamped with the read that decided the load rather
than with whatever happens to be open when the write lands. A write stamped before
an invalidation and landing after it is therefore not believed, and `Set` asks the
store for nothing to arrange it. `Group[V]` is decision 0028 §4's
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

## Limits

- **The store may never evict a generation counter.** A counter is written with no TTL
  and an absent counter reads as generation 0, so an evicted counter reopens every entry
  written under the generation a `Move` closed. `maxmemory-policy noeviction` (or no
  `maxmemory` at all) is the requirement; `compose.yaml` states it, and
  [docs/cache.md](../../docs/cache.md) says why. A `volatile-*` policy is safe — the
  counter has no TTL for it to take.
- **`Group[V]` has no in-tree consumer.** It ships, it runs the same conformance suite as
  everything else here, and the client-composition caches it was written for are in the
  applications that install these modules, not in this repository. `kit/httpx`'s host
  resolution is the only cache composed here.
- **One namespace granularity.** A `Move` closes a whole `Scope`, so an invalidation
  costs every other entry under it one reload. A finer generation, one per entry, is the
  same mechanism at one key per entry and waits on a deployment that measures the
  difference.
