# Cache policy

What a response may let a cache keep, by the surface it is served on and the kind of content it is. The kernel
applies this in one place — `kit/httpx/headers.go` (`secured.caching`) and `kit/httpx/surfaces.go`
(`assetCaching`) — so a module states nothing about caching unless it knows something the kernel does not, and
the kernel refuses what contradicts the table. Every row is held by a test in `kit/httpx`.

| Surface | Content | `Cache-Control` | Validators | Who may change it |
| --- | --- | --- | --- | --- |
| App (workspace) | any answer, page or JSON | `no-store` | — | a handler may add to it (`private, no-store`); a value **without** `no-store` is replaced with `no-store` and logged with its route |
| Ops (control plane) | any answer | `no-store` | — | as App |
| Public | a success to a safe method | `public, max-age=60` | — | the handler: it knows whether its bytes are somebody's own (`no-cache` for a page that must be revalidated, `no-store` for a sensitive one) |
| Public | a refusal, or an unsafe method | `no-store` | — | the handler, as above |
| any | a kernel asset (`Router.Static`) at the address naming its content (`?v=` = first 8 bytes of SHA-256, as `ui.Sheet` computes) | `public, max-age=31536000, immutable` | `ETag` = the same hash | nobody: a different body has a different address |
| any | a kernel asset at any other address | `no-cache` | `ETag` = its content hash; `If-None-Match` answers `304` | nobody |

## Why the session surfaces are absolute

Everything on App and Ops is served under a session and scoped to one tenant. A shared cache between the browser
and the kernel — a corporate proxy, a CDN someone put in front — that keeps one of those answers can hand one
tenant's page or list to the next person who asks the same address. So the kernel does not believe a handler that
loosens it: a contradiction of this table is a finding, logged at `WARN` as
`httpx: a handler set a storable Cache-Control under a session` with the surface, method, path and the refused
value, so it can be found and fixed rather than silently tolerated.

## Why assets are their own row

A kernel asset (`app.css`, the controllers under `js/`) is the same bytes for every person and every tenant of a
process. Under the session rule the workspace sent them `no-store` and every page view downloaded them again. The
stylesheet's URL already names its content (`app.css?v=<fingerprint>`), so at that address it can be kept for a
year; the controllers do not carry a fingerprint in their URL yet, so they are revalidated and a `304` costs a
round trip and no body.

## Why the value cache is one copy

The rows above are what a browser may keep. The table below is what **this process and its replicas** may keep,
and it is held by the same argument one level in: a belief that lives in one process's map is a different belief
in each of three pods, and an invalidation that reaches one leaves the other two serving what the installation
just stopped believing. `kit/cache` is the port for those values, `kit/cache/providers/valkey` the one store, and
every row here is held by a case in `kit/cache/cachetest` — which both adapters run, so the in-process store is
not allowed to be a different cache.

| Value | Where it may live | Lifetime | Invalidation | Forbidden |
| --- | --- | --- | --- | ---|
| host → tenant resolution (`kit/httpx`) | the shared store, one copy, no second copy in the process | 30 s (`hostTTL`) | `Move` of the host namespace — `InvalidateHost`, whatever hosts the change names (everything expires within `hostTTL` regardless) | caching a failure or an unknown host; caching the zero tenant; a second local copy, which would need a second invalidation; a `Delete` of the named hosts, which loses the load that raced the change |
| a composed value, per key | this process's memory, bounded at 16, LRU; its *marker* in the shared store | the entry's own TTL, and the marker expires with it | `Delete` of that entry's key, plus the TTL | composing eagerly for a host nobody asked for; caching a failed composition; a value that owns a resource needing release |
| a session, a permission grant, an entitlement | **nowhere** — not on this port, not in a process map, not in the store | — | — | every form of caching. `modules/auth/internal/kernel.go` says why: *"A permission cache is a window in which a revoked grant still works."* The read path is the transaction under RLS |
| anything a tenant owns | only through `cache.Of(tenant, …)`, the tenant from `tenancy.FromContext` or a `db.Tx[db.Tenant]` | the caller's own TTL | `Delete` of the key, `Move` of the namespace | a key without the tenant in it. `cache.Shared` is a greppable declaration that the entry belongs to the installation, and it is the only way to say so |

A `Move` and not a `Delete` is what makes an invalidation survive the racing load, and the host row is that
argument's only consumer in this repository: two replicas miss the same host, both call the loader, one finishes a
suspension and **deletes** the key, and the other writes back the tenant it loaded before the suspension began — so
every replica reads a suspended tenant for the rest of the TTL, which is the failure the row above refuses. Every
entry carries the generation it was written under, and `Get` answers the generation its own read found open so that
`Set` is stamped with **that** read and not with whatever is open when the write lands: a move during the loader
closes the answer that loader is bringing back, so it serves the one request already waiting for it instead of every
replica for the rest of the TTL. Its cost is coarseness — one suspension costs every host one loader query on its
next request — which for a handful of operator actions a day over an indexed query is the right trade, named in
`kit/cache`'s own comment on `Move`. `kit/httpx`'s `TestAnInvalidationDuringALoadLeavesNoResolutionBehind` runs the
suspension route's own call in that interleaving.

The generation counter is written with no TTL and is read as generation 0 when it is absent, so the store must never
evict it: under `allkeys-lru` or any other `allkeys-*` policy an idle counter is evicted, and the entries written
under the closed generation read as believed again. `maxmemory-policy noeviction` — or a store with no `maxmemory`
at all, which is what `make up` starts and what `compose.yaml` states — is the requirement; `volatile-*` policies
leave a counter alone, since it holds no TTL to take.

An installation with one process may leave `cache.adapter` empty and get the in-process store, which is a complete
deployment for one process; `kit/app` says so once at boot, because a claim nobody reads is not a warning. An
installation with more than one names `valkey`, and a store it cannot reach at boot refuses the start rather than
serving private answers from a cache its invalidations will never reach.

## Not yet in this policy

- Fingerprinted script URLs, which would move the controllers to the immutable row.
- A second consumer of the port. `cache.Group[V]` — the bounded lazy composition of decision 0028 §4 — is shipped
  and conforms, and nothing in this repository composes one yet: the client-composition caches this row was written
  for live in the applications that install these modules, so "a composed value, per key" above names a shape with
  no in-tree caller. `InvalidateHost` and `cache.Shared("host")` are the only uses in this repository.
