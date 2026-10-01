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
appname.Durable(app, "cart", "cart.checked_out")     // collect-cart-cart-checked_out
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

| Name | Until the rollout closes | Command that closes it |
|---|---|---|
| Subject, filter | `Filters` answers the app-scoped address and the two older shapes | once no process on an older build publishes, drop the entries after `Filter` and remake every consumer |
| Cookie | both spellings are read, `Cookie` is the one written | after one release, delete `PreviousCookies` and the aliases that read it (v1.3.0 with the aliases) |
| Durable | renamed by a migration that copies `platformkit_handled` and rewrites `platformkit_dead_letters` with it | the migration is the close: the old durable names are gone with it |
| Stored files | read at both physical paths | one-off move of `<dir>/<key[:2]>/<key>` under `<app>/<tenant>/` |

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
