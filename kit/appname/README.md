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
appname.PreviousStoragePath(fileID)                  // <2 hex>/<fileID> — where a release before the scope wrote it
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
| Durable | **the name is ready and the move is not**: `Durable` forms `<app>+<module>-<event>` from `Subscription.App`, which the composition stamps into every subscription it hands the transport (`kit/app`'s worker, from the composition's app — `Options.App`, or `nats.app` when the composition names itself no other way: the same line that names the job lock and the relay's claim), and because the join behind the app is the dash the name always carried, the scoped durable is the unscoped one with `<app>+` in front of it — the only form a stored row can be moved into, since the ledger holds no module or event name to rebuild from. Nothing moves the rows yet, so an installation that starts naming its app re-runs handled work and strands its dead letters — and a deployment that had already set `nats.app`, while `kit/app` named its consumers from an `Options.App` it was never given, finds this build is the moment those names move onto the slug | a drain `kit/events` owns and runs under system access: a `.up.sql` cannot (RLS empties its writes at the migrate role), and a `phase=data` drain cannot (both ledgers are keyed by `(event_id, durable)` and a drain windows over a single-column key) |
| Stored files | **not open**: `Local` writes `<app>/<tenant>/<key>` only when it was built with `NewLocalOf` — the tenant is the one `contracts.Scope` names, and a scope with none is refused before a path is formed rather than filed under the nil UUID. The two older positions are read: `<tenant>/<2 hex>/<key>`, which is what a deployment that names no app writes today, and the flat `<2 hex>/<key>` from before the port carried a scope. Two apps mounted at one root each list and remove under their own segment and never the other's — a store that names itself does not list the un-prefixed position at all, because nothing in that directory states which app wrote the bytes, so it reads its own older blobs there and leaves their removal to the `mv` rather than guessing at a directory it shares; and the sweep that does list that position removes only the bytes of the tenants whose `tenants.app` is its own app's (`modules/file/internal/reconcile.go`), so a named app's blobs are left there too until its `mv` | composition: nothing but a test builds a `NewLocalOf` today, so every installation stores at the un-prefixed position; and one app's older bytes stay where they are unless its boot moves them |

## Limits

What this branch does not do, in the order it costs:

- **The tenant control plane is scoped to the app.** `tenants.app` exists
  (`migrations/000043_tenant_app`), is stamped at the create from the composition's
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
  it with the composition's app (`Options.App`, or `nats.app` when the composition
  names itself nowhere else), and `events.Relay` is the same pass for a deployment that
  names no app). A row whose tenant row is gone names no app, so the app-less
  deployment keeps it and an app-scoped one leaves it: `LEFT JOIN` and `coalesce`
  say so, and `FOR UPDATE OF o` keeps the lock on the outbox rows, not the tenant
  table. Untested is a running two-app composition, not the claim.
- **The durable rename has no move, and the three obvious doors are each shut by
  something the repository owns.** `Durable` forms `<app>+<module>-<event>`, and the
  prefix property that any move needs is in place and tested
  (`TestADurableCarriesNoDot`). What is missing is the write. A schema `.up.sql` that
  rewrites the two ledgers across tenants is accepted and writes nothing at a migrate
  role that owns the tables and is no superuser, because `platformkit_handled`,
  `platformkit_dead_letters` and `tenants` are RLS-protected and the runner sets no
  system access around a schema file — `migrations/README.md` "A file that writes rows"
  and this package's own `review_r8_locale_backfill_role_test.go` are that measurement,
  and 000029 declines its own back-fill for the same reason. A `phase=data` drain, the
  one door the runner does open for a migration write, refuses both tables by name:
  `kit/db/backfill.go`'s `primaryKey` windows over a single-column primary key and these
  two are keyed by `(event_id, durable)`. And a file that raises the runner's own
  system-access marker itself — the cure that test names in the abstract — is refused by
  `scripts/check_gucs.sh`, which lets no file outside `kit/db` write a `platformkit.*`
  setting in Go or in SQL and exempts only `_test.go`. What is left is what that refusal
  points at: "a drain its owner owns, in a job" — a step `kit/events` runs under
  `db.RunSystem` at boot, renaming `durable` for the tenants whose `tenants.app` is this
  app's slug, which needs `tenants.app` (it exists) and a decision about where a boot may
  rewrite domain rows outside a migration.
- **Delivery reads the app back twice, and neither check is the other.**
  `transport.AddressMismatch(app, subject, ev)`, which the NATS provider calls
  before its sink runs, refuses a message whose *address* names another app, or
  names no app at all: it needs only the address the broker routed by, and it is
  what makes the wide `Filters` window safe to read at all. Behind it,
  `events.Consume` reads `tenants.app` for the tenant the document names
  (`holdsTenant`) and refuses a delivery whose tenant another app holds — before
  the handler's transaction opens, because that transaction opens *as* that
  tenant, and past it row-level security is the other app's and this app's handler
  code is inside. An address can say only that a publisher *claims* the delivery
  is this app's and this tenant's; a tenant id is the same vocabulary on both
  sides, so the claim is not the fact. A refusal runs no handler and writes no
  `platformkit_handled` row, so the event stays replayable for the app that does
  hold the tenant. What that read costs is one indexed row per delivery, and what
  it still does not close is the envelope's missing `app` field below.
- **The reference composition names no app.** `nats.app` (kit/config) is the slug's
  one configuration key and the reference application leaves it empty, which is the
  single-app deployment: every name it forms is the name it formed before this
  branch, and every tenant it creates is stamped with the empty slug. A deployment
  that does set it gets that slug through the whole composition: `kit/app.New` takes
  it as the composition's own app when `Options.App` names none, so one spelling puts
  the tenant stamp, the subjects, the relay's claim, the payload contract, the
  durables and the job lock together, and a composition that brings an `Options.App`
  spelling a different app is refused rather than left to serve one app and relay
  another's. Choosing the slug for `apps/platformkit`, declaring its `app.hosts`, and
  the browser test that signs in to two apps on two hosts of one deployment are the
  product's share. The split seen from the other side is closed: an `Options.App` that names
  an app while `nats.app` stays empty is the composition saying what it is, and `New` writes
  that slug into the `nats.app` of the configuration it carries, so the transport's subjects,
  subscription filter and connection name, and the app the migration places this boot's
  tenants against, name the app its payload contract, relay claim, durables and job lock
  already named. A caller's own `config.Config` is passed by value and keeps its empty key.
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
