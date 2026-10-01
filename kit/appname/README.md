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
appname.Durable(app, "cart", "cart.checked_out")     // collect+cart-cart-checked_out
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
| Durable | **closed by a migration, as the window asks**: `Durable` forms the scoped name from `Subscription.App`, and `migrations/000031_durable_app` moves both ledgers with it — every `platformkit_handled` row rewritten to the scoped durable and every `platformkit_dead_letters.durable` with it, each row to the app of the tenant that owns it, so one pass serves every app of a database that hosts many. The scoped name is the unscoped one with `<app>+` in front of it (`appJoin`, and `TestADurableCarriesNoDot` holds it), which is what lets a stored row be moved without knowing where its module ended | the file is the close: it applies once, and an installation that names no slug has nothing to move, because its durable did not change |
| Stored files | **not open**: `Local` writes `<app>/<tenant>/<key>` only when it was built with `NewLocalOf`, and it never reads the older `<dir>/<key[:2]>/<key>` position | one-off move of `<dir>/<key[:2]>/<key>` under `<app>/<tenant>/`, or a second read path in the adapter |

## Limits

What this branch does not do, in the order it costs:

- **The tenant control plane is scoped to the app.** `tenants.app` exists
  (`migrations/000030_tenant_app`), is stamped at the create from the composition's
  own slug, is never rewritten, and every control-plane read — `Get`, `List`,
  `ByHost` and the operator routes above them — filters on it. The back-fill proves
  its input: a tenant already in the table joins this app when every host it holds
  is one the boot declares (`app.hosts`), or when the operator named it in
  `app.tenant_apps`; anything else is listed by slug and the run refuses.
  `tenants_operator` is unique per app rather than per database.
  What that does *not* include: an installation-scope control plane that addresses
  every app of a deployment through one audited surface (the brief names it and
  this is not it — an operator reaches an app's control plane inside that app), and
  any placement of a tenant whose hosts and mapping both stay silent, which is a
  refusal by design and stays one.
- **The relay claims the rows of its own app** (`events.RelayApp`; `kit/app` runs
  it with `Options.App`, and `events.Relay` is the same pass for a deployment that
  names no app). A row whose tenant row is gone names no app, so the app-less
  deployment keeps it and an app-scoped one leaves it: `LEFT JOIN` and `coalesce`
  say so, and `FOR UPDATE OF o` keeps the lock on the outbox rows, not the tenant
  table. Untested is a running two-app composition, not the claim.
- **The durable rename's migration moves rows it can name.**
  `migrations/000031_durable_app` rewrites both ledgers, and it can only prefix what it
  reads, so a row whose tenant belongs to an app moves and a row whose tenant names no
  app stays — which is right for that installation, whose durable did not change, and is
  the whole of what is left unmoved. It is tested against fixture rows run by the role
  that owns the tables (`migrations/durable_app_rename_test.go`), and never against a
  deployment that really went through the window.
- **One boundary reads the app back; two more need a join.** Delivery
  refuses a message whose *address* names another app, or names no app at all
  (`transport.AddressMismatch(app, subject, ev)`, which the NATS provider calls
  before its sink runs): that check needs only the address the broker routed by,
  and it is what makes the wide `Filters` window safe to read at all. What still
  needs `tenants.app`, which now exists, is the check behind it: that the tenant the
  event names is one this app holds, which an address cannot say on its own.
- **The reference composition names no app.** `nats.app` (kit/config) is the slug's
  one configuration key and the reference application leaves it empty, which is the
  single-app deployment: every name it forms is the name it formed before this
  branch, and every tenant it creates is stamped with the empty slug. Choosing the
  slug for `apps/platformkit`, declaring its `app.hosts`, and the browser test that
  signs in to two apps on two hosts of one deployment are the product's share.
- **The slug's one key is `nats.app` and the deployment facts are `app.*`.** The
  split is history: the slug landed on the transport's section before the migration
  needed anything, and moving it now would rename a published key for no gain. A
  boot that sets `app.hosts` without a slug is the single-app deployment declaring
  where it is served, which is legal and is what places its existing tenants.
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
