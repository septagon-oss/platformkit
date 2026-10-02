# Changelog

## Unreleased

**A shared name carries the app.** `kit/appname` is now the one place a name two apps could share is formed:
the event subject and filter, the durable consumer, the job's advisory lock, the session cookie, a rate-limit
key, a stored file's physical path, the CloudEvents `source` and the broker connection name. A server hosts many
apps over one database and one broker (decision 0074 §6), and inside one app the tenant is the boundary — between
two apps a tenant id is only a label, because both sides name their modules with the same vocabulary. The name is
a validated slug rather than a string, and a census test scans the tree for any shared name spelled inline, so the
door cannot be bypassed by a caller who found it slower than writing the string. Every site the census named is now
behind a constructor: the transport, the relay's address, the scheduler's lock, the cookie, the limiter, the local
file store and the AsyncAPI document each take the app rather than forming a name. The slug arrives as one
configuration key, `nats.app`, and an unset one is the single-app deployment, which keeps every name it already
has. The delivery boundary reads the app back twice. `transport.AddressMismatch` takes the app, and an app that
names itself now answers only at its own scoped address — a message routed at the previous build's address names
no app, so it cannot be shown to be this app's, and is terminated rather than opened — and `events.Consume` then
reads `tenants.app` for the tenant the document names and refuses a delivery whose tenant another app holds,
before the handler's transaction opens: an address says only what its publisher claimed, and past that
transaction row-level security is the other app's. A refusal runs no handler and writes no claim, so the event
stays replayable for the app that does hold the tenant. The composition names every consumer it starts:
`kit/app` stamps `Subscription.App` with `Options.App`, so two compositions of one module do not bind one
JetStream consumer, one queue group and one handled-ledger key. And a stored file sits under the tenant whose
request wrote it — `modules/file`'s local store writes `<app>/<tenant>/<key>` and refuses a write whose call
names no tenant, because the key is a UUID and the path is the only thing that says whose bytes they are. The
rollout filter stays wide on purpose and the check is what decides. An unset slug keeps both older addresses, as
it always did.
**A tenant belongs to one app, and the control plane answers inside that app.** `tenants` gained a
non-null `app` column (`migrations/000041_tenant_app`), stamped when the composition creates a tenant and
never rewritten: lookup by host, the active-tenant list, `Get`, `List` and the operator routes over them
all filter on it, and `tenants_operator` is unique per app rather than per database. The back-fill proves
its input or refuses. A tenant already in the table joins this app when every host it holds is one the
boot declares in `app.hosts`, or when an operator named it in `app.tenant_apps`; anything else is listed
by slug and the migration writes nothing. `db.MigrateDeclaring` is the one door a boot has for a fact the
database does not hold, checked before a connection opens. A deployment that says nothing about itself
may still migrate a database with nothing to place — which is every fresh installation — and may not
migrate one with something to place and nothing to say about it.
**A durable is shaped so that its own rename is expressible.** A durable is half of the
primary key of `platformkit_handled` and `platformkit_dead_letters`, so a deployment that
starts naming its app leaves every row it wrote under a key its own subscription will never
ask for again: `DeliverAll` redelivers what the transport holds, the claim misses, and a
handler that already ran runs again — and a targeted replay deletes by the exact durable, so
the dead letters go invisible to the one command that reads them. `Durable` now forms
`<app>+<module>-<event>`: the app joins with `+`, which is in no app slug, module name or
event name, so `acme` + `billing` + `billing.plan.created` and `acme-billing` + `billing` +
`plan.created` are two consumers rather than one — and the join behind the app stays the dash
it always carried, so the scoped name is the unscoped one with a prefix, which is the only
move a ledger row allows (the table holds a durable and nothing else to rebuild it from).
Moving the rows is still owed, and the reason it is owed rather than done is now measured: a
`.up.sql` that writes across tenants writes nothing at the migrate role a real deployment
names, because the policies on `tenants` and both ledgers answer a schema file with an empty
set; the kernel's door for a migration write is a `phase=data` drain, which `tenants` can
have and neither ledger can, because a drain windows over the table's single-column primary
key and both are keyed by `(event_id, durable)`; and `scripts/check_gucs.sh` refuses the
shortcut a reader might reach for — the file raising the runner's own system-access marker.
T-0228; that move, the envelope's `app` field, the installation-scope control plane and the
reference composition's own slug are listed under *Limits* in `kit/appname/README.md`.

**A tenant signs its people in at its own issuer.** The installation had one issuer, one client and
one secret for the whole process (`kit/config.OIDC`, one `*oidc.Provider` behind a mutex): two
companies with two directories were one deployment, and one of them was wrong. The fact moves to the
tenant — six columns on `tenants` (`000030`), resolved per request inside the transaction the request's
own `Host` already resolved — and the mechanism stays in `modules/auth`, which asks its composition for
a provider through `contracts.OIDCProviders` and never learns that `modules/tenant` exists. Two
operator routes name a tenant's provider (`POST /api/v1/ops/tenant/tenants/{id}/oidc`, `…/oidc/clear`),
and the registration mode is the tenant's own column — `disabled`, `existing`, `provision` — not a
process-wide flag. The row holds `oidc_secret_ref`, the *name* of where the secret is and never the
secret, because a row is copied into the outbox and from there into the audit trail. See
[modules/auth](modules/auth/README.md) and [modules/tenant](modules/tenant/README.md).

**A second factor gates every door that opens a session, and a person's sessions and keys are theirs
to read and revoke.** TOTP enrols with the secret shown once and is proved before it is trusted; a
recovery code spends once and rotation replaces the set; the password leg now halts with a 401 that
says the password was right (`ErrFactorRequired`), and the door a tenant's own provider walks a person
through asks the same question of the same account, so a provider that proved only the first half opens
nothing — the refused person finishes at `POST /api/v1/auth/challenge/verify` with their own code, and
the one withdrawal that is refused is a person's last factor.
`GET /api/v1/auth/sessions` lists what a person has and revokes one or all, one `auth.session_revoked`
per row its own statement removed, at `/app/auth/sessions` in the shell. `POST /api/v1/auth/tokens`
mints a bearer key scoped to permissions its holder already has, which dies at its expiry, is not a
credential at another tenant, and cannot widen itself back to its holder — and `httpx.AnyCredential()`
is the fifth authorization declaration for the one operation that names no permission and spends none,
because the catalogue is the first request a client that is not a browser makes and the only credential
it holds is a key. `000031` and `000032` are the two tables; `modules/auth/internal/surface_test.go`
pins the module's whole surface against the record the kernel's own mounts write. **No passkey**: the brief asked for one, it is not in this change, and the module README
says so under *Open here*.

Re-recording follows in the same branch: `apps/platformkit/testdata/openapi.json` gains these sixteen
operations, and rule B6 of its wire gate refuses a change of a door's authorization that could refuse a
caller it used to admit — the one widening it allows, `signed_in` to `any_credential`, is named in the
table beside it.

**A denial names the permission and who can grant it.** The page a signed-in person sees when a guard refuses
them used to say only "Não pode fazer isto." / "You can't do this", although the guard's detail carried the
permission it asked for. It now keeps that verdict and adds "You need the <permission> permission for it. Anyone
who manages roles here can grant it: ask your administrator.", in the request's language. A shell whose catalogue has only the
short sentence keeps its language and gains the permission's name. From three UX walkthroughs on 2026-09-30, in
which a staff member could not tell what to ask for; T-0184 adds asking for it.

**A card with an address is a link.** `components.Card` rendered an anchor only when `Clickable` was set as well
as `Href`. A card with `Href` and `Hoverable` got a hover shadow over an `<article>` that went nowhere, which was
every card on the pets adoption list: a UX walkthrough found an adopter could not open a single animal's page.
`Href` now makes the card a link with the clickable styling, and `Clickable` alone still styles a card that
handles its own interaction.

**An event leaving the outbox is a CloudEvents 1.0 envelope, and the tenant is
in its address.** The wire form was `transport.Event`'s own struct tags —
`{"id","name","tenantId","payload","at","actor"}` — a private shape nothing
outside this repository could read. It is now CloudEvents 1.0 in structured
content mode, with `tenantid` as a required extension attribute and
`traceparent`/`tracestate` as the distributed tracing extension, and the subject
is `platformkit.<tenant>.<module>.<event>`: a tenant's backlog is an address, a
durable can be per tenant, and a bridge routes a customer without opening the
payload. The envelope decodes the pre-envelope shape and never writes it; a
subscription answers both addresses while that window is open, because a NATS
`*` is one token and an unread decoder is not a window. `module.Module.Declared`
names the Go type of each payload; that projection is the JSON Schema the outbox
refuses a mis-shaped payload against — a member the projection cannot describe
constrains nothing, and it no longer panics the door either — and the AsyncAPI
3.0.0 document `apps/platformkit/testdata/asyncapi.json` is rendered from it, as
each message's `payload` where a validator reads, and checked in by
`make check`. A manifest outside this repository keeps the list it already wrote:
`Events` still takes event names, and `Declared` is the field that takes
`events.Declare[contracts.Changed](contracts.EventChanged)` beside them. An event named
without a type is published unchecked and counted as uncovered — the state a nil payload
already meant — and a module that wants its payload in the document, and refused at the
outbox before the row is written, declares it. `events.Replay` is the operator's verb for a terminal delivery, and
`kit/trace` carries the W3C context from a request into the outbox row — it
collects and exports nothing, which the metrics pillar still owns. That carrier now
bounds the caller's `tracestate`: `trace.Parse` keeps whole entries up to
`trace.MaxTraceState` (512 bytes) and drops the rest, because the string is stored in
the outbox row and republished on every event the request caused, so an unbounded one
is a header paid for per event; the trace itself survives either way, and a state too
large to keep is dropped whole rather than cut mid-entry. See
[the rollout notes](kit/events/README.md): the subject change recreates every stored consumer, and the claims are what make that safe.

A replay now requires its actor as it requires its reason, and the record it
refuses to write is the one that would have named nobody. `Purge` leaves an
outbox row a dead letter still describes: the row is the payload's only copy, and
a terminal failure the operator can read but never run again, with nothing left
saying what it carried, is the evidence this change set exists to stop losing.

**A test that must name a tenancy setting to read it can be exempted from the GUC gate, in a reviewed row.**
`scripts/check_gucs.sh` reads text, so a test that matches a migration's `set_config('platformkit.…'` line as
a string looks like a write. `scripts/gucs-exempt.txt` lists such files, one `<path> <reason>` per line.
Only a `_test.go` file may be listed, since a test never ships, and a row without a reason is refused. Every
exemption is printed on every run. Assembling the name from pieces to slip past the gate remains the wrong
answer, because the gate then reads nothing.

**A module declares the addresses it moved, and the kernel redirects them.** `module.Module.Moved` is a list
of `{From, To}` whole paths. The kernel answers an old address, or anything under it, with the same redirect
as its own migration table: 302 for a safe method, 307 for a write, the remainder and the query kept, never
cached, no body. The surface gate refuses a route mounted where a row redirects. `module.Validate` refuses a
relative path, a row pointing at itself, and one old address claimed by two modules. A module that moves a
page now writes one row, not a handler for the old address (T-0104 had written 988 lines of them across
eight modules).

**A module's own workspace pages stand instead of the generated register.** The admin shell used to mount
generated screens for every registered resource, even at addresses a module already served with its own
page. The surface gate refuses two routes at one method and path, so such an application did not start.
The pets client could not boot for exactly that reason (T-0126). The shell now leaves a resource's screens
to the module when the module recorded any GET at or under that resource's screen address, and logs it at
boot.

**Every page's stylesheet is four cascade layers, and a consumer sheet gets exactly one
of them.** `ui/css` emits `@layer` again: `ui.Compose` writes `@layer tokens, base,
components, client;` as the sheet's first statement, puts the palette and role variables
in `tokens`, the preflight in `base`, every resolved class list — the components' and
the extras' together — in `components`, and every hand-written sheet a module or an
application hands it in `client`. A layer ranks before specificity, and for normal
declarations a later layer wins, so the client layer is the strongest of the four and
what keeps a consumer's rule off a component is the gate below and not the ranking
above: a consumer can neither write a later file nor name a more precise selector to
win, because the name is refused first. The gate at
that boundary is `ui.Compose`: a consumer sheet that states an `@layer` of its own,
carries a brace, a comment start or `</style`, begins a rule with an at-keyword or a
`;`, names a kernel-rendered attribute or one of the classes the sheet's own
markup carries — by a `.`, or by comparing the contents of the `class` attribute —,
`:root`, a `--pk-` property or a raw colour — each read as a name, in any
spelling a browser resolves it to, and a colour in its hex form or in any
functional notation (`rgb()`/`hsl()`, `lab()`/`lch()`, `oklab()`/`oklch()`,
`hwb()`, `color()`, `color-mix()`) read over the value a browser computes, with the
argument of a `url()` reference and the contents of a quoted string stepped over
because neither computes a colour — is refused by panic — such a sheet is Go source
wired at mount, and a refused composition ships no bytes rather than a stylesheet that
lost a layer. The gate is the contract of the sheet a page links, and `ui.Compose` is
the one place it is enforced: `ui/export` composes the same four layers with
`ui.ComposeDesign`, which places every rule identically and refuses none, because a
design capture measures what a browser computes for a sheet a mount would refuse — an
authored colour with no token yet, a margin on the icon of a component someone is
proposing — and the design-export suite, which is where that distinction showed, is a
CI step rather than part of `make check`. That is a breaking change for a sheet written against an earlier
kernel: composed against this tree, exported consumer sheet builders in the products
checkout refuse that compose against the pin they carry today, each on a raw colour in
a `box-shadow` or a
`background-image`, or on a selector that names a kernel hook or one of the kernel's
utility classes; a sheet assembled inline sits outside that count, and the count
itself belongs to the sweep's own checkout rather than to this one.
The cure is the client's own share — `css.VarRef` for the colour, its own hook or its
own class for the selector (T-0139, T-0126).

**The reference application's personas are declared, and each is proven to do its own
journeys and be refused the others'.** `apps/platformkit` seeds `coordinator`
(`task:read`, `task:update`) and `observer` (`task:read`) with every tenant, beside
auth's `admin` and `member`. It refuses to compose if a persona grants a permission
nothing declares or one that belongs to the operator. `persona_test.go` drives seven
journeys as each of the four roles at the shipped composition and holds a table of
allow/deny. Resolving another person's task is refused to the administrator and the
coordinator by the task policy, not by a grant.

**Object scope is decided by a policy the composition writes, and every refusal is
audited.** `kit/tenancy/providers/opa` embeds Open Policy Agent (Apache-2.0) as a
`tenancy.Policy`: a composition writes its rules in Rego, they are compiled once at
start, and a decision names its revision, the source's content hash. An undefined
decision is a refusal and an answer that is not an object with a boolean `allow` is an
outage, never an allow. The reference application composes the task module with
`apps/platformkit/policy/task.rego` — an assigned task is its assignee's to resolve —
so the object-scope question decision 0011 asks is answered by a running rule and not
only by a hook. `tenancy.WithPolicyRefusals` lets the HTTP layer observe each refusal
`tenancy.RequirePolicy` returns, so a `POLICY_DENIED` from deep in a module's service
reaches `security.denied` with the action, reason and revision, as `AUTH_DENIED` does.

Two of these entries, one property: a tenant keeps somebody who can administer
it. Each was written because the state was reachable, not because a race was
reported, and each says below what it leaves open rather than leaving that to a
reader who depends on it.

**A migration now says what kind of migration it is, and the runner holds it to
that shape.** A rewrite and a ten-million-row backfill were the same file: one
transaction, no bound, and the expand/contract rule a review comment. A file may
carry a `-- pkit:` header — `phase=expand|contract|data`, with `batch=`, `table=`,
`autocommit=`, and `allow=<rule> reason=…` for the exceptions a reviewer reads
(the marker is read whatever spaces are written inside it, because an unread
declaration is a file applied as a kind it is not) — and the runner then applies it
in the mode it declared: transactionally, as one
nontransactional statement that must be re-runnable, or as a backfill wrapped over a
window of its table's primary key, one committed transaction per window, resumable
from the last key it committed in `schema_migration_backfill` — and from the whole
table when no key is committed yet, which the ledger says as a NULL, because a `text`
key can hold the empty string and the smallest key there is. A drain ends in the
transaction that wrote its last window, not in one after it: "every row written" and
"the version applied" are one commit, and no run can stop between the two and leave a
table that reads as unfinished work. Every file runs with a
five-second `lock_timeout` (configurable, `database.lock_timeout`) and no statement
bound by default — the same budget the worker's batches re-assert, because a backfill
is the fifty transactions that wait behind the running application, not the one file —
and one stopped by the lock budget returns `db.ErrContended`:
nothing new was applied, and it may be run again. A `contract` file refuses while the
`expand=` version it names has not already applied, and nothing of an owner applies
past a drain that has not finished. Which drains a release finishes, and which its
worker's `schema-backfill` job does, is decided by what waits behind the file: a data
file with files behind it is the worker's, because this run cannot reach those files
either way, while the owner's last pending data file is drained by the run itself under
the bound it gives itself — so a boot never waits behind a table it cannot empty, and a
release whose last step is to fill a column does not leave that step to a tick. A body
that says it bounds itself has no window to count, so it stays the worker's even last.
A boot that meets a drain
already in flight resumes it under that same bound and boots whatever the bound leaves:
`db.ErrBackfillBudget` out of a migration is the worker's to finish, not a failed
deploy, while `platformkit migrate` and `Bootstrap` — doors asked to finish — still
report it. The worker's own drain is bounded now too, at ten thousand windows: a tick that
could not stop was a tick that never ended, holding the job's advisory lock while it repeated
work. A refused data file leaves no progress row behind, and the corrected file
then converges through the same door, because that row is what
a resume reads and a file refused for its shape never ran — and the three shapes that
are the window's to refuse are named there rather than answered with a server error: a
body of two statements; a body whose own CTE list binds the window's name `batch`; and a body
that writes the column the cursor is ordered by, which leaves the table never empty of work. The
last two are read as constructs, not as one way of writing them — the CTE name in either spelling
PostgreSQL takes it, with a list inside a sub-expression left to the scope that shadows the window
there, and the assignment target in either shape it takes it, a key merely read by the body left
alone — and what those readings cannot see (an upsert's `DO UPDATE SET`, a body that only appends
rows above the cursor) is ended by the tick's bound rather than by nothing. A body that merely opens with a CTE list of its own is none of them,
and is drained: the window joins the body's list, since PostgreSQL takes one `WITH` per
statement. The rules
the runner refuses before connecting, the keys, and the floors each source declares are
written down once in [migrations/README.md](migrations/README.md); the mode-scoped ban on
nontransactional SQL is the amendment to
[ADR 0011](docs/adr/0011-migration-ownership.md). A rule that documents no exception
cannot be excepted: an `allow=` naming one is refused as the bypass it is. The guards
read operations rather than spellings (`ALTER TABLE t ALTER col TYPE` rewrites the
table whether or not the optional `COLUMN` keyword is there, `ALTER TABLE t DROP col`
takes a name away from the running release whether or not the file spelled the keyword,
and the `DEFAULT` that
makes an added column ordinary is read from that column's own definition and not from
the file), a rule floor is bounded by the source's own highest version rather than by
whatever number a manifest carries, and the guard and the
executor read one normalised text of a data file's body, read where PostgreSQL reads it
(a `--` inside a value is data, the apostrophe inside a `/* … */` is commentary, a
`$tag$ … $tag$` body is one value and an `E'…'` closes past its escapes), so an excepted
body runs once and a body that names its window in another case still gets the window.

**The two things the runner cannot decide now have doors.** `platformkit migrate`
applies the pending schema and exits, over exactly the sources, floors and budgets
every role's boot composes — the retry for a file that came back `db.ErrContended`,
which is an operator's decision to wait rather than the runner queueing inside the
advisory lock every other replica waits on; `--drain` finishes a backfill instead of
waiting for the worker's tick. Every applied file and every finished drain logs the
duration the runner itself measured, and `make rehearse`
is [scripts/rehearse_migrations.sh](scripts/rehearse_migrations.sh): the step a
release runs before it publishes, which lets the previous release's own binary migrate
a fresh database, seeds ten thousand rows per table into a copy of it, applies this
tree's pending files against that copy while sampling `pg_stat_activity` for lock
waits every 100 ms, and exits 0, 1, 2 or 3 — applied, failed, could not run, over
budget or contended. It prints a failed migration's own message the moment it stops,
before any query of its own can fail over a copy the candidate never migrated, and it
refuses to report a measurement it did not take: a watcher that fell short of half the
samples its own watched window resolves to is `LOCK WATCH BROKEN` and exit 2, and every
report names the window it watched and the tree its binary was built from — uncommitted
files included, because `go build` compiles them and `git diff` does not see them. A
copy that could not be dropped is named as
`LEFT BEHIND`. A rehearsal that could not run exits non-zero rather than passing
quietly.

**The user screen cannot take away a tenant's administration.** Setting the sole
administrator's roles to none, deactivating them and deleting them each answered 2xx,
and each left a tenant where nobody inside it could change a role again: whoever was
left standing was answered 403 by the roles screen and by
`PUT /api/v1/auth/roles/{name}`, because the write they now needed was the one the
tenant could no longer authorise. Those three writes now answer 422 and name the rule.
The rule is `user/contracts.CheckedAdministration`: a tenant keeps at least one
**active** person holding a role that grants `role:manage`. It asks two questions with
two predicates on purpose — `User.Administers` is wide, so removing somebody who has
not accepted their invitation is a write the floor looks at, and `User.CanAdminister`
is narrow, so an unaccepted invitation cannot hold the floor up. The narrow half is
measured rather than theoretical: in this repository's default configuration, which
ships no `mail.host`, inviting an heir as an administrator and standing down answered
200 while the heir's sign-in answered 401. Active rather than "has a password",
because `active` is exactly what `Service.Open` tests before it writes a session, so
the predicate and the thing it predicts agree by construction; counting passwords
instead would refuse writes in a composition that provisioned identity-provider
accounts with no hash, which nothing here creates.

The floor never refuses a grant, so the state stays one grant away from repair; in a
tenant where every holder of an administering role is still unaccepted, every removal
of one of them is refused until somebody active is appointed. `internal.Service.floor`
takes the advisory lock below after the subject's row lock and before its reads, so
two administrators standing down at once cannot both pass. The real service and the
`usertest` fake call the same decision, and the conformance suite holds both to it.

`user.Deps.Administration` is a new **required** field: a composition without it
panics at `user.Module`, as `file.Deps.Storage` does — and so does an adapter
handed to it with nothing inside, because a `&contracts.AdministrationFunc{}` looks
wired and would answer "nobody administers this tenant" to every question the floor
asks. `auth.AdministeringRoles` is what goes inside
`&usercontracts.AdministrationFunc{Ask: …}` — who holds a role is the user module's
table, what a role grants is the
auth module's, and the application joins them rather than either module reading the
other's rows. `Administration` is a one-method interface and the adapter is taken by
pointer, which is what keeps `user.Deps` comparable as it was published at v1.1.0: a
func value reached through an interface satisfies a checker and then panics the first
time two `Deps` are compared.

**The roles screen the navigation always named exists, and it has a floor of its own.**
`/admin/auth/roles` was declared by `modules/auth` and served by nothing, so every
installation logged `admin: a nav entry leads to a path no route serves` at boot and
every operator saw a menu item that answered 404. `modules/admin` now serves the
screen that entry names, guarded by the same `role:manage` the two JSON routes
declare: what each role grants, as ticks over the permissions this tenant may name,
and one button per role that writes the whole list back through `SetRole`. An operator
permission is offered only in the operator's own tenant, because that is what the
write accepts, and a grant the composition no longer offers is named in the form that
would drop it rather than hidden and then destroyed. The list pages at
`ui/resource.PerPage`, as every generated list does. Applications wire
`admin.Deps.Roles`, which stays optional: nil mounts no screen, and a composition
whose navigation names the entry while wiring nothing is told so at boot.

Emptying the last role that grants `role:manage` left nobody able to change a role
again, through the screen or through the route. The rule is
`auth/contracts.CheckedAdministration`, so both doors refuse it and the conformance
suite holds every implementation to it, and `internal.SetRole` takes the advisory lock
before its first read rather than after the read that decides. `ValidRoleName` also
bounds a name at `contracts.MaxRoleName`, the 64 the route and the form already
advertised and nothing underneath them enforced.

**One lock, and what it does not buy.** Both floors take
`"administration/<tenant id>"` through
`pg_advisory_xact_lock(hashtextextended(key, 0))` — the same literal written out in
both modules rather than imported from one, pinned by a test on each side and by a
test across them. That makes the two writes queue behind each other, in both orders,
which is a real defect closed, and it is nothing more: **the two floors do not
compose** into the property they are both for, which is that somebody who can still
sign in holds a role granting `role:manage`. Nothing asks that question. Two sequences
reach a tenant nobody can administer with every individual write permitted and no
concurrency involved — create a role granting `role:manage`, give it to nobody, then
empty the role everybody holds; or, with two administrators holding two such roles,
strip one person's roles and then empty the other's role. The first is
`TestTheTwoFloorsStillDoNotComposeIntoOneInvariant`, which asserts the hole and fails
the day somebody closes it. The symmetric fix is named in
`auth/contracts.CheckedAdministration` and is not in this release: auth would ask the
composed question the way the user module already does, through a narrow capability
the application supplies, so one property is checked once instead of two halves of it
being checked separately.

How bad a lockout is depends on whose tenant it is, and the repair path is narrower
than "there is one". `POST /api/v1/tenant/tenants/{id}/invite` runs in a system
transaction and provisions somebody holding a role the application names — `admin`
here — so an operator can put an administrator into a *customer's* tenant whose people
lost their grants, through a supported route and without SQL. It does not help where
the named role is itself the one that was emptied, since that is the role it hands
out, and it does not help in the operator's own tenant at all: the route declares
`tenant:manage` as an operator permission, held through that tenant's own roles, and
there the control plane shuts and SQL is the only way back. That route is not the only
door of that shape either —
`POST /api/v1/tenant/tenants/{id}/suspend` against the operator's own tenant is a
single request after which every operator host answers as though no site were served,
and `modules/tenant` has no route that reverses it. That predates this work and
neither floor touches it.

The floor sees rows and not people: an active administrator who has forgotten a
password in a tenant with no mail, or whose identity provider is gone, is a lockout
nothing here reports. It is per tenant, so the operator's own tenant gains no extra
protection from it. Nothing here renames or deletes a role, so a held name cannot
vanish today; a module that adds either will need its own floor.

`usertest.NewFake` keeps the signature it published in v1.1.0, and a fake built that
way has no role system, exactly as it did before the floor existed. A test that wants
the floor asks `usertest.NewFakeWithAdministration`, and `TestFakeConforms` runs the
shared suite through it, so the fake and the real service cannot disagree about the
floor quietly. That shape is a release constraint, not a preference: this package is
published, and [RELEASE.md](RELEASE.md#choose-the-compatible-release-line) measures a
break against v1.1.0 with no accepted-break baseline, so changing this signature would
owe the `/v2` migration it describes.

`TestEveryNavEntryLeadsSomewhere` makes the unserved-nav class a gate rather than a
boot warning. Two entries are named as known debt with their reason —
`modules/file`'s `/admin/file/files` and `modules/audit`'s `/admin/audit/events` — and
the gate fails if either becomes served and is left in the list. Serving them needs a
read-only mode in `httpx.Resource` and `ui/screens`, which mount create, edit and
delete unconditionally today.

`make check-race` is called by CI. The goal and the Makefile target have been here
since the concurrency kernel, and CONTRIBUTING has said CI runs it, but no workflow
called it: the gate that would catch a lock released before the commit that needed it
had never run outside a contributor's shell. ci.yml now runs it after `make check`,
and this change widens `RACE_PACKAGES` to `./modules/auth/internal/...` and
`./modules/user/internal/...` beside the defaults, because that is where the advisory
locks above live.

Two direct dependencies moved, and one test helper had to be told why. `dave/dst` goes
to v0.28.0 and `jackc/pgx/v5` to v5.11.0, each on the requirement line Dependabot
proposed; the one graph move beyond them is dst's own `dave/jennifer` 1.5.0 → 1.7.1,
which `go.sum` carries and nothing in `./...` links. Nothing unrelated came with them.
pgx 5.11 rewrote its connection-string
parser to match libpq, which reads `+` as a literal, and `modules/task`'s isolation
DSN was built with `url.Values.Encode`, which writes an encoded space as `+` — so
all 34 `TestConcurrentTaskCommands` subtests refused to connect, asking PostgreSQL
for an isolation level called `repeatable+read`. Percent-encoding the space is
correct on both pgx versions, so that is a repair here rather than something the
bump has to work around. What could reach a consumer is dst's own floor: it moved to
`go 1.26.0`, which this module clears and a pinning module on an older toolchain
would not.

**A command-owned field is refused where a body is read, an empty `PATCH` says
nothing, and a write asks whose row it is.** `refuseImmutable` asked whether the
decoded map held the
declared key while `encoding/json` binds a field a key merely folds onto, so
`{"Author": …}` wrote the author through every generated create route of every
module declaring `Immutable`. The five doors that read a body — create and patch,
the two a page calls beneath HTTP, and `Values` beneath a create form — now ask
it with `strings.EqualFold`, the comparison the decoder itself makes, and refuse
the whole write naming the declared field rather than the spelling it arrived in.
A `PATCH` that named no column moved `updated_at` and published
`<module>.<entity>.updated` for a change nobody made; it now validates nothing,
writes nothing, publishes nothing and returns the row as read. The tenant-scope
recheck a body naming a column triggers still runs first, so an empty `PATCH`
of a row outside the request's tenant is 404 like any other body. The `DELETE`
asked that question of nothing at all: row-level security filters a delete by its
`USING` clause alone, since a delete produces no new row for a `WITH CHECK`
clause to inspect, so on a table every tenant may read, one tenant deleted
another's row — 204, event and all — or answered 500 out of the policy violation
where the Spec soft-deletes. Both doors now ask `crud.RecheckTenant`, the tenant
compare `Update` already made, exported rather than copied: the change's one new
exported name, and a nil entity answers it `ErrInvalid` as every other door in
`kit/crud` does. `db.Now` stamps the three timestamp columns, so an
answer carries the instant the column holds. What stays open: a `Singleton`
declares no command-owned field.
**The address an operation is mounted on now says which surface it belongs to, and a
module no longer writes one.** A module's `Routes` receives `httpx.Surfaces` — the
public face, the workspace and the control plane — and the router composes the address
from the surface and the module: a relative path that repeats either is refused at mount,
so `/api/v1/tasks/tasks` cannot be written again. The workspace keeps its JSON addresses;
what moved is where its documents are (`/admin/<module>/<entity>` is now
`/app/<module>/<entity>`, the shell at `/app/admin/…`), and the anonymous doors of
`auth`, `content`, `file` and `site`, which took `/api/v1/public/…`. Each surface has its
own chain: the public one reads no session, sets no cookie (a handler that mints one gets
`PUBLIC_SETS_A_COOKIE` withheld at the writer at every body size, and a 500 of that
name wherever the response can still be replaced) and is cacheable for sixty seconds;
its anonymous writes are counted by tenant, route and address, so one customer's
office does not exhaust another's budget;
A page whose body its owner can republish asks that cache to revalidate rather than answer
from what it kept, so a publish is on the site the next time the page is loaded;
the workspace is `no-store`, `noindex`, and admits an anonymous caller only at a route
that declares `Public()` — and boot prints those doors rather than keeping a list of them;
the control plane is served at `server.installation_host` and answers as an unmounted
address at every other host — the same status, the same body and the same headers every
other refusal of that host carries, because the host gate runs inside the middleware
that writes them — including to a tenant that is not the installation's, which
is the second half of the tenant-API incident recorded in
[ADR 0015](docs/adr/0015-a-refusal-has-one-value-and-two-shapes.md)'s neighbourhood and
in [`modules/tenant`](modules/tenant/README.md). `modules/tenant`'s routes and
`modules/billing`'s plan catalog therefore moved to `/api/v1/ops/…`.
`GET /api/v1/admin/resources` is now `GET /api/v1/app/resources` and is mounted by
`kit/app`, with the document supplied by the composition
(`app.Options.WorkspaceCatalog` = `screens.Describe`); every catalog entry gained a
`screen` key, a `write_path` key and a command a `path` key — each only where the
derivation from the entry's own `path` is no longer true, so a document that could
always be read the same way still is. The workspace is described as an empty surface
in OpenAPI, stamped `x-platformkit-surface` per operation.

What a caller sees: `/admin`, the admin module's own pages beneath it (`/admin/login`,
`/admin/health`, `/admin/assets`, `/admin/_gallery`), `/api/v1/admin` and the four
public doors answer with a 302 (307 for a write, never a cached 301) for one release,
and are deleted in v1.3.0 — see [aliases.go](kit/httpx/aliases.go).
`/api/v1/tenant/tenants` deliberately has no alias: a control plane does not announce
itself by leaving the old door open at every customer's host. Refusals now name a code
in the JSON `detail` — `AUTH_ANONYMOUS`, `AUTH_DENIED`, `AUTH_NOT_OPERATOR`,
`AUTH_NO_TENANT`, `AUTH_PRINCIPAL_CHANGED`, `CSRF_ORIGIN`, `PUBLIC_SETS_A_COOKIE`,
`LIMIT_EXHAUSTED` (the public surface's own write limit) and
`WRITE_ELSEWHERE` (a write of a resource whose writes are served on another surface,
answered at its read door, naming `write_path`) — where the sentence used to be a
lowercase prefix.

- The kernel answers every refusal it makes for itself in the shape the requester asked
  for: the problem document to a client that wanted a value, the shell's own page — in the
  request's language — to a client that came to be shown one. This includes an address
  nothing is mounted at, an address mounted for other verbs, a CSRF failure and a panic
  that escapes a handler. A guard's refusal — a missing session, a missing grant, a row of
  another tenant, a route the plan excludes, the public surface's write limit — answers the
  same way: it is the same decision, and it is the one a *navigating* person was being
  shown JSON. An htmx write counts as a client that parses a value: its controller
  (`ui/assets/js/htmx-config.js`) reads the refusal's code out of the problem body and
  swaps nothing for a 4xx, so a page answered there would reach nobody.
- Those codes are what a refusal *page* is translated by. `ui/page` holds the one table
from a code to a catalog key (`fault.<CODE>`, and `fault.<status>` for the 404, the 405
and the 500, which carry no code because the kernel wrote the sentence), the page is
negotiated from the request's
`Accept-Language` — a guard answers before a session, a tenant or a stored preference
exists to ask one — and it carries `Content-Language` and `Vary: Accept-Language`. A
translated page keeps the code in front of the sentence (`AUTH_DENIED: Não pode fazer
isto.`), because the code is what a person reads back to support. The
declaration follows the sentence on the page: a shell with no catalog, or none for that
code, says the kernel's English and declares `en`. Two narrower promises came with it:
the pointer to a split resource's write door now goes to a caller holding the
credential that door reads, because the door would refuse anybody else the moment they
reached it; and a tree mounted with `Static` answers a missing file, its own prefix and
any directory beneath it through its surface's chain, in the shape the client asked for,
instead of net/http's plain-text note and a listing of the shell's filenames. The hourly
purge of the rate-limit counters moved from `modules/auth`'s sweep to `kit/app`, beside
the outbox's, because the kernel's public write limit made the kernel the table's second
writer. See
[ADR 0017](docs/adr/0017-three-surfaces-by-path.md) for what this costs a module.

**A module's schema opens to the application role without either of them naming it.**
`apps/platformkit/postgres-init.sql` is where the read role is given `USAGE` on
`public` and its default table and sequence privileges, so a module migration
that created a schema of its own produced a namespace only its owner could open
and every later query answered 42501. The module cannot grant its way out — a
module's SQL may not name a deployment's role — and the migration runner already
refuses to, decoding grantees out of an object's own ACL when it revokes
application access to the ledger. `platformkit_module_schema(owner)`, in the new
`migrations/000026_module_schema.up.sql`, is the one line a module's first schema
revision runs instead: it creates the schema named by that owner and hands each
role the migration role already grants defaults to *in the namespace that schema
opens beside* the privileges that deployment pinned for it there — grantee and
privilege list both read out of `pg_default_acl` rather than written down, one
namespace's rows and no union of several, so a role pinned to `SELECT` stays a
reader inside a module's schema and a role pinned only in some other namespace is
handed nothing by this function — a database-wide pin still reaches a module's
tables, because PostgreSQL applies it there and not because this mirror copied
it. No table moves here — the reference modules
keep theirs in `public` — and the check that every table is scoped to a tenant
stops holding a second copy of its own query:
`dbtest.TenantTablesSQL` is exported, gained the schema column, and matches a
schema against the ledger's owners instead of `current_schema()`, which was the
only schema until this. `migrations/README.md` states the consequences a
deployment can hit.

**A refusal now has an owner that imports nothing.** `kit/fault` declares
`ErrNotFound`, `ErrInvalid` and `ErrConflict`, and `kit/crud` re-exports those same
values rather than declaring its own, so the adapter that classifies a driver error
and the value package that refuses a write before a transaction exists hold one
object and `rest.Fault` answers 404, 422 and 409 once for either name. The package
exists because the alternative was a link: a module's `contracts/`, `events/` or
`domain/` package needing one of the three had to import `kit/crud` — gorm, a
driver, a `db.Tx` in every signature — for an error value. It links the standard library and
nothing else, which its own test asserts against `go list -deps`, the package gate
holds to an empty allowance, and that gate's fixture refuses as out of bounds if it
ever reaches `kit/db` — the edge back into `kit/crud` is an import cycle the
compiler refuses before any gate sees it. The messages do not move and they are not
inert: all three are still what a log line carries, `crud: invalid` and `crud:
conflict` are what a bare refusal reaches a client with as its problem detail, and
`crud: no such row` is not — a 404 is answered with the sentence `kit/rest` holds
for a row this tenant cannot see — while the literal `crud: invalid: ` is what
`kit/rest` trims off a detail to derive the field message a client reads, and
`modules/admin` asserts a person is never shown that prefix: the prefix names the
package that used to own these values, and renaming one is a wire change with
consumers on the other side of it. Eleven of this repository's `contracts/`
packages wrap one of the three under the adapter's name: three of a module's own
contract source — `modules/content/contracts` and `modules/file/contracts`, which
wrap `crud.ErrInvalid` for a body the Markdown renderer refuses and for bytes
that are not the type they were uploaded as, and `modules/user/contracts`, whose
own `ErrRegistrationExists` is a wrap of `crud.ErrConflict` — and the eight
shipped test-support packages one directory deeper, which refuse the way the
service each stands in for refuses. No file of `modules/auth/contracts` imports
that adapter now: this change moved it to `fault.ErrInvalid` and retired its
direct link. `kit/fault/README.md` names all eleven and what each still costs it. No
closure moves for any of them — each names `db.Tx` and imports `kit/crud` or
`kit/db` for `crud.Base` and a transaction-aware service of its own. The
reachability runs from the module to the kit package:
`modules/auth/contracts` reaches `kit/crud` through `modules/user/contracts`,
which imports it for `crud.Base` and not for a sentinel, and reaches `kit/db`
through four of its own files' service signatures; no kit package reaches a
module, which the compiler and `./scripts/check_imports.sh` both refuse. So
nothing here is a gate that now passes. What moves is adoption: the
alias has a value package wrapping the owner rather than only a name, and a
downstream consumer's value package does the same when its pin moves.

## [1.1.1] - 2026-09-18

A tooling patch. No exported API moved and no shipped behaviour moved: the diff from
v1.1.0 is one browser assertion under `tools/designexport/openpencil` and seven lines
of [RELEASE.md](RELEASE.md).

It exists because the Gitea run for v1.1.0 (88) concluded **failure** on the step
*Native browser observations*, and that step drives a file shipped inside this module:
anybody who checked out the tag and ran `npm run test:browser` in
`tools/designexport/openpencil` got the same `TimeoutError`. The suite now follows the
label-to-field association instead of asserting the literal control id
`pk-textarea-description`, which v1.1.0's namespace change had replaced. The full
browser suite is 265 of 265.

The prose change is the part that outlives this defect. `make check` and `make e2e` do
not run `test:browser` — the CI job does, as its own step — and when it failed on
v1.1.0, everything after it was skipped, including `make e2e` and the budget ratchet.
So RELEASE.md now states that an unread CI result is not a green one and that a release
is not ready while the verdict for the exact commit cannot be read. v1.1.0 was tagged
with that verdict unread: that, not the assertion, is the defect being repaired.

## [1.1.0] - 2026-09-18

**This release is not source-compatible with v1.0.0.** The pinned comparison
reports 71 exported changes between them — `kit/crud`'s row types are
`kit/entity`'s, `httpx.Document/Fragment/Script` and `design.CSS` moved into
`ui/page` and `ui`, `events.Memory()` became `memory.New()`, `migrations/` is the
kernel's schema alone, five methods joined `user/contracts.Registrations`, and
`ui.Compose` returns a `Sheet`. No `Deprecated:` shim repairs the 29 that are type
*moves*: apidiff resolves an alias and still reports
`contracts.User.Base: changed from crud.Base to entity.Base`, verified against a
two-revision module before this decision was taken.

It was taken deliberately rather than by omission. No consumer sits on the v1.0.0
stable line — the commercial catalog and the client application both pin
pseudo-versions of `main`, which establish no compatibility — so a v1.1.0 that
keeps the number breaks a build nobody has. It also breaks one nobody has *yet*:
`go get -u` from v1.0.0 now lands here and fails to compile, so **pin an exact
version**. What is owed and not yet paid is unchanged: an outside consumer needs
the `/v2` module path and import migration described in
[RELEASE.md](RELEASE.md#choose-the-compatible-release-line), and v1.1.0 is now the
tag every compatibility question is measured against. The
[accepted-break baseline](scripts/PUBLIC-API.md) that stood in for a release line
went with this tag; the line has no accepted break from itself.

The kernel stops deciding what it does not own. `ui/document` and `ui/resource`
render a document and a resource's screens from values — no database, no router
— and `ui/page` and `ui/screens` stay as the adapters that read a request and
carry the kernel's rules; `kit/entity/display` owns how a value reads, with
`kit/rest` delegating. `kit/app` no longer imports an event provider: the
application supplies `app.Transports{Memory, JetStream}` and the kernel selects
by name, refusing at `New` when the selected name has no constructor.
`events.Memory()` is removed — call `memory.New()`. Each reference module ships
its own SQL under `modules/<name>/migrations` and adopts the history the
foundation applied for it, so an existing installation is re-owned by checksum
and nothing re-runs; `migrations/` is the kernel's schema alone.
`scripts/check_packages.sh` records every new boundary. `ADR 0013` explains why
`kit/module` stays a typed manifest.

The composition layer becomes values a second shell can call. `ui.Compose`
returns a `Sheet`; `ui/page` holds `Chrome`, `Request`, `View`, `Frame` and
`Navigation`, with `page.Serve` as the one adapter between a handler and the
router; `ui/screens` is the seven generated pages of a resource as pure
renderers plus `Mount`, and `screens.Describe` publishes the same knowledge as
JSON at `GET /api/v1/admin/resources` for a shell that is not a browser.
`modules/admin` is composition only. Controllers read the sign-in path off
`<html>` and name no route; the confirm dialog's inline handler, which the
content security policy blocked, is gone. Ceilings re-baselined; two packages
join the binary.

## v1.0.0

The extracted reference architecture replaces the 0.x CLI scaffolder that lived
at this module path (releases to v0.15.1). The 0.x line is kept reachable under
the `legacy-0.x` branch and its tags; nothing from it is imported here. What
v1.0.0 is: `ARCHITECTURE.md`. What it promises about size: `loc-budget.json`.
