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
suite both adapters run, and its `Store` factory is handed the case rather than a name:
two calls made with one case are two handles onto one store, and every case is handed a
store it owns alone — so a backend with one connection has to hand back a handle that
closes only that handle and addresses a keyspace of the case's own, which is why the
suite's close case is last.

Sessions, permission grants and entitlements are never cached here, in this process or
in any store: `modules/auth`'s own kernel says a permission cache is a window in which
a revoked grant still works. The read path for those is the transaction under
row-level security. [docs/cache.md](../../docs/cache.md) holds the table.

## Application names and reads

Use `MemoryFor(appname.Name)` for a fresh in-process cache, or
`NewFor(appname.Name, Backend)` for a backend you already own. The zero name
means a single app and retains an empty first segment in each physical key:
`:33333333-3333-4333-8333-333333333333:host/example.com`. A named app retains its
prefix, for example `pkit:33333333-3333-4333-8333-333333333333:host/example.com`.
Generation keys use the same prefix and end in `host#`. The runtime's configured
fallback remains `pkit`; adopting these APIs does not migrate existing keys.

For a new shared application identity, use [appname.Parse](../appname/appname.go).
Cache constructors retain [Slug](appname.go)'s older grammar for nonempty names,
including digit-first names such as `1legacy`, supplied as `appname.Name("1legacy")`.
That exception preserves existing cache namespaces; it does not make such names
valid cross-package identities. Using appname's key builder for them would discard
their app token. Unifying the grammars requires a separate data migration decision.

The string constructors remain supported as deprecated delegates:

| Existing API | Migration |
| --- | --- |
| `Memory(app)` | Use `MemoryFor(appname.Name(app))`. Both own a fresh backend and panic on invalid nonempty names. Only the new API accepts an empty name. |
| `New(app, backend)` | Use `NewFor(appname.Name(app), backend)` and handle its error. Only the new API accepts an empty name. |
| `MemoryNamed(app, backend)` | Use `NewFor(appname.Name(app), backend)` with the **same backend**, handling the returned error or explicitly retaining a composition-time panic. |

Construction performs no backend I/O. A refused construction neither calls nor
closes the supplied backend. Successful handles retain the existing ownership:
`Close` releases that backend, including when another handle shares it. There is
no reference counting; pass a non-nil backend and manage its lifetime accordingly.

`GetEntry(ctx, c, key)` wraps one `Cache.Get` in `Entry{Value, Found, Generation}`.
Use `Found` to distinguish a hit, including empty bytes, from a miss; stale bytes
may accompany a miss. Pass the miss's `Generation` to the fill's `Set` so a `Move`
during the load still invalidates that fill. Errors pass through unchanged, with
no retry, byte copy or stronger lifetime guarantee than the supplied cache. Do not
use an erroneous answer as a successful read. The original `Cache.Get` method and
interface remain supported without deprecation. The executable
[example](example_test.go) shows a miss, its stamped fill and a hit.

## Limits

- **The store may never evict a generation counter.** A counter is written with no TTL
  and an absent one reads as generation 0, so an evicted counter loses the record of
  every `Move` and reopens what the first one closed. `maxmemory-policy noeviction` (or
  no `maxmemory` at all) is the requirement; `compose.yaml` states it and
  [docs/cache.md](../../docs/cache.md) says why. A `volatile-*` policy is safe — the
  counter has no TTL for it to take.
- **Composed values are recomputable.** `modules/picture` uses `Pool[V]` over
  Group's retention engine for a total bound across tenant keys. Both retain
  immutable values only; source access is checked before asking for bytes.
- **One namespace granularity.** A `Move` closes a whole `Scope`, so an invalidation
  costs every other entry under it one reload. A finer generation, one per entry, is the
  same mechanism at one key per entry and waits on a deployment that measures the
  difference.
