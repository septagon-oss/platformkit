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

## Not yet in this policy

- A shared cache port with the tenant in the key type, and host-resolution and client-composition caches on it so
  an invalidation reaches every replica (brief T-0119, part 3).
- Fingerprinted script URLs, which would move the controllers to the immutable row.
