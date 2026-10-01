# kit/appname — the app is in every name two apps could share

A server hosts many apps over one database and one broker (decision 0074 §6). Inside one app the tenant is the
boundary; between two apps a tenant id is only a label, because both sides name their modules with the same
vocabulary. So every name the kernel derives carries the app: where an event is published, which consumer owns
it, which job lock it takes, which cookie a browser sends back, which rate-limit bucket a write spends, where a
stored file's bytes sit.

## The one door

```go
app := appname.MustParse("collect")

appname.Subject(app, tenantID, "cart.checked_out")   // platformkit.collect.<tenant>.cart.checked_out
appname.Filter(app, "cart.checked_out")              // platformkit.collect.*.cart.checked_out
appname.Durable(app, "cart", "cart.checked_out")     // collect+cart+cart-checked_out
appname.JobLock(app, "purge")                        // collect/job:purge
appname.Cookie(app, "session", true)                 // __Host-collect-session
appname.RateLimitKey(app, tenantID, "writes")        // collect/<tenant>/writes
appname.StoragePath(app, tenantID, fileID)           // collect/<tenant>/<fileID>
appname.Source(app, "cart")                          // /collect/cart
appname.ConnectionName("platformkit-worker", app)    // platformkit-worker/collect
```

A name formed anywhere else is a bug: `census_test.go` scans `kit`, `modules`, `apps`, `ui` and `tools` for the
inline spellings of each of these names, allows only the sites it lists with a reason and an exact line count, and
proves its own patterns on `testdata/planted`, which forms all eight by hand. An allow-list entry is debt — when a
site is migrated the entry has to go, or the census refuses the list that no longer matches the tree. The sites
that still spell a name inline are listed there as `pending`: this package is the door every one of them moves
behind, and the census is what stops a new spelling appearing while they wait.

`Name` is validated rather than a string, because the alternative is a slug read from configuration and
published as a broker subject: `Parse` refuses anything a subject token, a consumer name, a cookie name and a
path segment could not all hold. A `Name` built by conversion that is not a slug forms an address whose app token
is empty, which no app's filter matches: the failure is a message that reaches nobody, never a wildcard.

## Windows

Each window is the shape `transport.Filters` and `httpx.LegacySessionCookies` already establish: read the old
name, write the new one, and name the command that closes it.

The windows below are open for the names whose scoping this branch shipped. A row
that is not open says so: reading the old name is a property of the code that reads
it, and where no code reads an old name yet, no window is open, whatever a table
elsewhere claims.

| Name | Status | Command that closes it |
|---|---|---|
| Subject, filter | **open, and checked**: `appname.Filters` answers the app-scoped address and the two older shapes, and `transport.AppFilters` hands all three to the consumer, so a build that names its app reads what a build that does not published. `transport.AddressMismatch` takes the app and accepts only its own scoped address, so the two older shapes are read and then refused — an address that names no app cannot be shown to be this app's. The window is therefore one-directional: a deployment must not start naming an app until no process still publishing at the older addresses is alive, because from that moment its own older traffic is unreadable to it | once no process on an older build publishes, drop the entries after `Filter` and remake every consumer |
| Cookie | **not open**: `httpx.CookieName` still writes `__Host-<base>` for the deployment that names no app, and `CookieNameOf` is the constructor a caller with a slug uses; `httpx.SessionCookieOf` reads `__Host-session`, `session` and the `platformkit_session` aliases, which is `PreviousCookies` for those two bases and no app-scoped name | wire the auth module's `Cookies` to a slug, then one release, then delete `PreviousCookies` and the aliases that read it |
| Durable | **not open**: `Durable` forms the scoped name, `kit/events` forms it from `Subscription.App`, and no migration copies `platformkit_handled` or rewrites `platformkit_dead_letters` from the old names, so a deployment that starts naming its app re-runs handled work and strands its dead letters | the migration is the close: `000030_*` copies every handled row under the new durable and rewrites every dead letter's `durable` with it |
| Stored files | **not open**: `Local` writes `<app>/<tenant>/<key>` only when it was built with `NewLocalOf`, and it never reads the older `<dir>/<key[:2]>/<key>` position | one-off move of `<dir>/<key[:2]>/<key>` under `<app>/<tenant>/`, or a second read path in the adapter |

## Limits

What this branch does not do, in the order it costs:

- **The tenant control plane is not scoped to the app.** `tenants` has no `app`
  column, so there is no fact for `Get`, `List`, `ByHost` or the operator routes to
  filter on, no back-fill that can prove every existing tenant belongs to the
  composition booting the migration, and the operator index is still unique per
  database rather than per app. This is the brief's item 5 and the review's third
  HIGH; it needs a migration, RLS, the tenant module's Deps and its own tests, and
  it is the next piece of work here, not a window.
- **One boundary reads the app back; two more need the tenant row.** Delivery
  refuses a message whose *address* names another app, or names no app at all
  (`transport.AddressMismatch(app, subject, ev)`, which the NATS provider calls
  before its sink runs): that check needs only the address the broker routed by,
  and it is what makes the wide `Filters` window safe to read at all. What still
  needs `tenants.app` is the check behind it — that the tenant the event names is
  one this app holds, which an address cannot say — and the relay's claim, which
  has no app to join on until that column exists.
- **The reference composition names no app.** `nats.app` (kit/config) is the slug's
  one configuration key and the reference application leaves it empty, which is the
  single-app deployment: every name it forms is the name it formed before this
  branch. Choosing the slug for `apps/platformkit`, and the browser test that signs
  in to two apps on two hosts of one deployment, are the product's share.
- **The envelope carries no app field**, so `AddressMismatch` reads the app out of
  the address the broker routed by and not out of the document. A body that names
  an app is a second fact to check against the first, and it is a field the kernel
  does not write yet; what is refused today is the message whose *routing* belongs
  to another app, which is the half the broker can witness.
- The cache key (`CacheKey`) has no caller yet — T-0119 owns the cache.
- The census exempts one reviewer-owned pin (`kit/events/transport/review8_…`)
  rather than scheduling its edit, and it counts a name spelled as a whole literal
  only outside `_test.go` files, where stating the name a browser is expected to
  send back is an assertion and not a name the runtime forms.

## What stays shared

The `PLATFORMKIT` stream (its subjects are app-scoped), the dead-letter table (its rows carry a scoped durable),
the composition migration lock and migration owners (one schema, so scoping them would let two apps migrate it at
once), CSRF (origin checks, no token), and the process's telemetry `service.name` (one process, many apps — the
app is an attribute on each record, `appname.App`).

## Reused, Added, Made reusable

**Reused** — the rollout-window shape `kit/events/transport`'s `Filters`/`legacyAddress` and `kit/httpx`'s
`LegacySessionCookies` already establish, which every window above copies rather than invents; `db.TryLock`, whose
only argument is the name a job locks; and the caller-list source scan of `kit/fault/review4_caller_list_test.go`,
which is the shape the census takes. **Added** — `Name` and the constructors, because no type in this kernel
carried which app a process was serving (`grep -rn 'type Name string' kit` was empty at the merge base) and every
shared name is formed from tenant and module alone today, so nothing existing could hold a name nobody could yet
spell. **Made reusable** — the census itself: a counted allow-list with a planted case, which any "written down in
exactly one place" rule in this repository can copy, and `appname.App`, the one attribute name every log line,
audit record, metric and span will use to say which app it belongs to.
