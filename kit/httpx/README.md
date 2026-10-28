# HTTP kernel

`kit/httpx` builds the one Huma API the application serves and enforces that
every operation declares its authorization. `New(Options)` returns the `*API`
and the router; `Register` mounts a handler with an `Auth` — `Public()`,
`SignedIn()`, `AnyCredential()`, `BearerKey()`, `Permission(key)` or
`OperatorPermission(key)` —
and `ValidateDeclarations` is the boot gate that refuses a route without one. Read
[httpx.go](httpx.go) for the middleware chain: which surface an address is, host
to tenant, the lazily opened tenant transaction (`TxFrom`, `ConnFrom`), the
request on the context (`RequestFrom`), the security headers, the per-request
nonce (`Nonce`) and what a page may declare about its own response
(`AllowWASM`).

## Exact signed integers

Typed JSON bodies accept an `int64` as either a decimal string or an exact
integer number, including integral decimal/exponent spellings. Fractions,
overflow and padded strings are refused with 422. Ordinary `int` and `int32`
fields remain numbers. Custom JSON/text codecs and protocol media retain their
own representation; opaque JSON is not interpreted as a declared integer.

Numeric `int64` **output representation is deprecated**. During this release
line ordinary `application/json` still returns numbers. Send
`Accept: application/vnd.platformkit.int64+json` to receive decimal strings,
including counts and revisions. A wildcard does not select it; equal explicit
preferences keep legacy output. Both request content types accept both input
forms. Responses vary on Accept. Keep strings or use exact integer arithmetic
in clients; converting a large value to a floating-point number loses digits.

With documentation enabled, `/openapi.json?int64=string` describes the complete
input union and string/int64 responses. Generate adopting clients from that
view and configure the explicit Accept header. The default document preserves
the numeric contract and adds the selectable success media. A keyed retry must
select the original response representation: changing it returns 409 without
executing the command again. Recover the original answer or read the resource.
The [next-major record](../../docs/compatibility/v3.0.0.md) owns the default cutover.

### Composition

**Reused** — `Register`, the Huma registry and JSON engine, REST field binding,
request transactions and idempotency replay retain their existing ownership.
**Added** — exact lexical int64 binding, API-local schema projections and explicit
representation negotiation, because float-based map decoding cannot preserve
the full signed range and the legacy output contract must remain available.
**Made reusable** — the private [JSON body codec](../internal/jsonbody/codec.go)
binds typed bodies and REST map/form values with the same integer decision,
with shared real/fake precision cases. No public codec API is added.

A module's `Routes` receives `httpx.Surfaces` — three routers, one per surface —
and mounts on the one that fits the door. It never writes a prefix: `Prefix`,
`Path` and `PagePath` compose an address from the surface and the module, and
`Register` refuses a path that repeats either. See [router.go](router.go) and [mounted_routes.go](mounted_routes.go).

## One page may run WebAssembly

`htmlPolicy` refuses `WebAssembly.instantiate`. `script-src` carries `'self'` and
this request's nonce, and both speak about where a script came from; a module
compiled from bytes came from nowhere, so the policy that lets the shell's own
tagged script run still refuses the thing such a page was written to do.

A page that needs the capability says so — `httpx.AllowWASM(ctx)`, in the shape of
`Nonce(ctx)` — and *that response's* policy gains `'wasm-unsafe-eval'` inside
`script-src`. Nothing else moves: the nonce, every other directive, and the whole
policy of every page that said nothing are the bytes they were before the
declaration existed, which is what [wasm_test.go](wasm_test.go) holds the pair
to. Three consequences are the design:

- The token is a constant of [headers.go](headers.go) and the declaration takes no
  argument beyond the request, so no module can ask this file for
  `'unsafe-inline'`, a source expression or a host.
- A handler that set a `Content-Security-Policy` of its own keeps it: a
  declaration may widen the kernel's default and never a handler's stricter one.
- The claim lives on the response. It is set-once, per request, unreadable from
  outside this package, and worth nothing after the status is written — a page
  that streamed past the response buffer keeps the policy it was given.

[ui/page](../../ui/page/serve.go)'s `View.RunsWASM` is the field a module author
writes against, and `Serve` calls this for the view it actually rendered: a refusal
page and a redirect instantiate nothing, so neither carries the token.

Reused: `(*API).headers` and `(*secured).policy` — the repository's only writer of
a policy; `nonceKey` and `Nonce` as the shape of a per-request declaration; the
per-response state `headers.go` already carried for `markStatic`; the `httptest`
fixtures of [headers_test.go](headers_test.go) and ui/page's
`TestAnInlineScriptCarriesThePolicysOwnNonce`.

Added: `AllowWASM`, the two constants it composes from, `document.View.RunsWASM`
and the one line in `Serve` that reads it. Nothing existing could carry this: the
policy is one constant whose only variation is the nonce and the surface's
`object-src`, so editing it widens every page; `Page.ContentSecurityPolicy` makes
the author restate the whole policy including the nonce, which is exactly how a
copied storefront once lost it; and a general per-directive policy builder would be
a new API with one consumer.

Made reusable: the second shape on the nonce pattern — a per-response claim a
handler writes through one exported, ctx-shaped function and `policy` reads — which
the next response-level declaration composes instead of reinventing, and
`View.RunsWASM`, the field the editor pages of decision 0086 will declare through.

## Three surfaces

| Surface | Serves | JSON address | Document address |
| --- | --- | --- | --- |
| `Public` | a tenant's face to an anonymous visitor | `/api/v1/public/<module>/<rel>` | `/<module>/<rel>` |
| `App` | the workspace: the generated shell and a module's own values | `/api/v1/<module>/<rel>` | `/app/<module>/<rel>` |
| `Ops` | the installation's control plane | `/api/v1/ops/<module>/<rel>` | none — refused at mount |

The empty module is the composition's own namespace (`/api/v1/app/resources`,
`/app`): the kernel's routes, which no capability composes because the address
belongs to the composition rather than to a module. Only `kit/app` asks for it.

The public surface's JSON prefix says `public` and its document prefix does not:
a tenant's anonymous page answers at `/<module>/<rel>`, because the address a
visitor is given is the tenant's own site and not a partition of this
installation's — the module that claims the public root answers published slugs
at `/` and `/{slug}` beside it. That is also why `module.Validate` refuses the
module name `public`: a module of that name would compose `/public/…`, the one
address every other module is refused at mount for naming.

Each surface has its own chain, and the difference is not cosmetic:

- **Public** resolves no session and reads no cookie — a request holding one is
  answered as the anonymous request it is — sets no cookie (a handler that mints
  one is a 500 named `PUBLIC_SETS_A_COOKIE`, withheld at the writer at every body
  size, and a 500 with the body discarded wherever a response is still there to
  discard), caches a safe 2xx for sixty seconds, and puts a limit on an anonymous
  write because it is the one surface with no account to lock out
  (`Options.WriteLimiter`, counted by tenant, route and address). A refusal from
  that counter is a 429 whatever the counter's own reason was — a spent window and
  an attempt the counter could not record are refused here, and only a store that
  cannot be reached at all is the outage this kernel fails open on
  ([kit/limit/README.md](../limit/README.md) owns the four).
- **App** recognises the caller, refuses an anonymous caller except at a route
  that declares `Public()` — and `AnonymousDoors` lists those, because "the
  workspace admits nobody by default" is only checkable once you can see who it
  admits — and answers `no-store` with `X-Robots-Tag: noindex`. A route here may
  also declare `WriteLimits`, the counted sibling of the anonymous arm
  (`route_writes.go`): the counters declared, evaluated in that order and keyed
  through `kit/limit`, which scopes every key by tenant, so one caller's own window
  is spent before the tenant's shared bucket is touched at all. It runs after the
  permission gate and before the body is read, so a caller who was never allowed
  spends no allowance, and it refuses with 429 and the `RetryAfter` hint this kernel
  puts on a refusal that knows how long to wait — the same header a route whose own
  capacity is full sets for itself with `httpx.RetryAfter`. A limiter that cannot
  answer lets the request through and logs, which is the anonymous arm's rule and
  stated twice because it is two arms and not one.
- **Ops** is served at `Options.Installation`'s host and nowhere else. Anywhere
  else it answers exactly what an address nobody mounted answers — the same
  status, the same body, and the same headers every other refusal of that host
  carries, because the gate runs inside the middleware that writes them, so the
  surface discloses nothing; and at the installation's host a tenant that is not
  the installation's is refused the same way, before the `Authorizer` is
  consulted. `app.Installation{Host}` is the deployment's fact, read from
  `server.installation_host`.

A credential that carries its own authority is held to it at **both**
declarations that spend authority, and that pair is the whole of what makes a
scoped key mean something: an operation naming `Permission(k)` is refused unless
`k` is on the list, and an operation declaring `SignedIn()` — the door about the
caller themselves, which names no permission and so spends its caller's whole
authority — is refused outright. A key narrowed to a list therefore never
reaches the doors that manage the credentials a person holds, and can never
widen itself back to its holder. `scoped_credential_test.go` is both halves,
with the session caller as the control.

The one operation that names no permission because there is no permission to name
— because it spends none of the caller's authority and only describes what the
caller may already reach — declares `AnyCredential()` instead, and a scoped key is
admitted to it. The set holds the two apart because the answers differ: the
catalogue at `GET /api/v1/app/resources` (mounted by `kit/app`, the kernel's own
namespace) is built by asking the authorizer which resources *this caller* may
read and write, so the document a narrowed credential receives is already narrowed
by that credential, and refusing it would leave a bearer key able to do its work
and unable to find the routes its scopes name — the first request of a client that
is not a browser. Mounting a self-service door here is the escalation `SignedIn()`
exists to refuse: the test at such a mount is that the operation reads grants and
never spends them, which is why no module's route belongs there and today only
`kit/app` has one.

`BearerKey()` is the door whose subject is the key itself — retiring the key that
authenticated the request. It admits a principal with a `TokenID` whatever its
scopes, refuses a session with 403, and is the one 401 in this package: a request
with no recognised key is challenged `WWW-Authenticate: Bearer`, because a key is
the only credential that door accepts. `bearer_key_door_test.go` pins the three
answers; [ADR 0020](../../docs/adr/0020-a-credential-may-retire-itself.md) says why.

`Home` claims a surface's root for one module — the site's home page at `/`, the
workspace's at `/app` — and reports to the second claimant that it did not take
it. `accepted` is the closed table of which declaration each surface will host; a
contradiction is collected at mount and reported by `ValidateDeclarations`, so
the process never listens on a route whose address and chain disagree. A refusal names
itself with one of the published `Code*` constants (`AUTH_DENIED`, `LIMIT_EXHAUSTED`,
`WRITE_ELSEWHERE`, …) in the `detail` and in the log, and every guard answers it in the
shape the client asked for: the problem document for a value, the registered renderer's
page for a navigation (see [ui/page](../../ui/page/README.md)). An htmx write is a client
that parses a value — its controller reads the code and swaps nothing for a 4xx — so it is
answered in JSON even though it asks with `Accept: text/html,*/*`.

A refusal that becomes a page resolves the host it is a page *of* (`withHostTenant`,
called by `show`, the one branch either refusal path takes when a renderer is registered).
Which languages a request is answered in is the tenant's declaration and not the
browser's, and the guards that answer ahead of routing — an address nobody mounted, the
verb an address does not take, a cross-site write, a handler that panicked — run ahead of
the operation middleware that resolves hosts, so the page they render would otherwise be
worded by whoever brought the header. A host nobody serves has no tenant to declare a
language, and that one refusal is negotiated from the request alone; a request whose
context is already over gets no query at all.

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it
rebuilt. **Reused:** `Surfaces` is a typed wrapper over the two `chi.NewMux`
routers `New` already built, with the mount gate beside `ValidateDeclarations`;
the anonymous write limit is `kit/limit`'s counter table — the one
`modules/auth` already shares — whose purge moved beside the outbox's sweeper in
`kit/app` rather than being given a second one; a refusal's value is
`kit/problem`'s document and its page is `ui/page`'s `Messages`, `SelectLocale`
and `Formatter.Text` over the headers `Serve` already wrote; the cases reuse
`guardKernel`, `dbtest.Schema` and `documentFault` instead of new doubles.
**Added:** three routers per host with a chain each, the mount gate that refuses
a route contradicting its router, the installation-host gate
(`Options.Installation`), the one-release alias table and the `Home` claim — none
of these had an owner before, because one chain answered every address and which
surface a route was on lived in prose; and `WriteLimits`, the counted arm of the app
surface (`route_writes.go`), which is the windows a route declares beside itself —
one person's, one tenant's — evaluated in that order behind the permission gate and
before the body is read, beside `RetryAfter`, the one way any refusal in this kernel
says how long to wait. **Made reusable:** `Router.Prefix`,
`Router.Path` and `Router.PagePath`, so a module composes an address instead of
spelling one; `SurfaceExtension`, the `x-platformkit-surface` key every recorded
operation carries; `LegacySessionCookies`, which the auth module needs to clear
what the kernel still reads. A test says what it protects in its own name and in the
comment above it, never in the name of the round that asked for it.

A module may also declare an absolute address of its own — `At`, for a protocol
that names its own address, SCIM's `/scim/v2` — and that moves where the router
stands, never what it carries. The router keeps its surface, so a workspace router
keeps the session, the cookie, `no-store`, the tenant transaction and the
deny-by-default authorization; a control-plane router keeps its installation-host
gate; a public router keeps a chain that resolves no session and sets no cookie. The
credential rules are the same table they were before the method existed, so the
routes a module may mount at a declared address are exactly the routes it may mount
inside the table. Only the address is the protocol's.

### One release of the old addresses

[aliases.go](aliases.go) is the whole migration: `/admin` → `/app` and the
four pages the admin module owns there (`/admin/login` → `/app/admin/login`,
and the same for `/health`, `/assets` and `/_gallery`), `/api/v1/admin` →
`/api/v1/app`, and the public doors of `auth`, `content`, `file` and `site`
under their own prefix. A row is a redirect — 302 for a safe
method, 307 for the rest, never a cached 301 — and never a second mount, so the
mount gate refuses anything that answers at a redirected address. Every row is
deleted in v1.3.0, one release after they shipped. What is deliberately absent is
`/api/v1/tenant/tenants`: a control plane does not announce itself by leaving the
old door open.

Pages are operations too: `HTML` mounts one, `Page` is its response shape,
`SeeOther` and `Redirect` end a write, `LocalPath` is the one rule for where a
browser may be sent, and `SignIn` names the form an anonymous visitor is sent
to. Rendering markup into a `Page` belongs to [ui/page](../../ui/page/README.md)
(`Render`, `RenderFragment`, `InlineScript`); this package imports no markup
library. `RegisterResource` and `Resources` are how [kit/rest](../rest/README.md)
publishes an entity so the admin generates its screens and catalog.

`Static` mounts an asset tree at the address the surface composes for it — inside
the surface's chain, so a workspace tree is `no-store` and `noindex` and a public
one is cacheable — and records it in `Mounted` beside every route, because a
composition's audit of what it serves has to include the files it serves. It is
refused at mount on `Ops`, which has no document address (the tree would compose
a public-looking prefix and answer at every customer's host), and refused at a
namespace's root, where the routes of that namespace answer: `Static("/assets",
tree)`, never `Static("/", tree)`.

An alias is spent ahead of the router, so no route exists at the old address to
serve a body from: a screen served at two addresses gives one form two namespaces
(`ui/forms.Namespace` is derived from the path), which is how a controller starts
writing to the wrong page. It is never a 301, because a permanent redirect a browser
caches outlives the release that removes the row — the person then gets a 404 from a
cached answer with nothing left in the application to fix. 302 for a safe method and
307 for the rest, so the method and the body survive and nothing is remembered past
this visit.

Each row states the composition it is written for, because the doors an alias aims at
are mounted by modules this package may not import: a row about an optional door
nobody composed is a redirect into a 404, and only the reference application's running
server can answer, row by row, whether the address each row points at answers.

### Mounting beside another module, and crossing tenants

`Router.In` answers this surface's router bound to another module's namespace —
the workspace screens of the tenant resource belong at `/app/tenant/tenants`
whoever generated them. The namespace arrives as a typed value the owning module
exports from its own `contracts` package (`tenantcontracts.Namespace`,
`authcontracts.Namespace`), so a mount that names an owner holds a reference to
it: renaming that module breaks the caller's build instead of inventing a
namespace that answers 404. A namespace this composition never registered answers
`false` rather than panicking, because a shell that mounts a page beside an
absent module's screens has to answer for that page itself and keeping its own
router is the answer. `Router.ForModule` and `Router.Known` are the same two
doors by name and are deprecated; the generated register still uses them, because
the module whose resource it renders is read off the catalog rather than named in
source.

A module that has work no tenant transaction can do says so in its manifest
(`module.Module.SystemAccess`, and ADR 0006), and `Surfaces.SystemToken` then
hands it the credential minted for that module, carrying that module's own
reason: the audit trail of every `db.RunSystem` says who opened it and for what.
A token taken without the declaration is refused at composition naming the module
and the field to add, and the returned credential is `nil` as well — the
capability is not handed out on the strength of a call nobody declared. A router
borrowed into another module's namespace is refused too, naming both modules:
the credential belongs to the module being wired, so take it on your own router.

## A write that is sent twice

Two clients can send one write twice — a phone that gave up and retried, a curl
whose operator pressed the up-arrow — and at `POST /api/v1/task/tasks` a second copy
of the answer is the work done twice. Two headers live here; the store behind one of
them lives in [../idempotency](../idempotency), and the counter behind the other in
[../limit](../limit).

**The header.** A write that carries `Idempotency-Key` is answered from a
[record](../idempotency/idempotency.go) rather than run again: the same key, the same
body and the same caller get the first attempt's status, `Content-Type`, the
navigation it sent the caller to (`Location`, or `HX-Redirect` for the request htmx
made — html.go's `Redirect` picks between them) and body, and the handler does not
run. The ceilings it was decided under go with it too. The record keeps the answer's own
`RateLimit-Policy` line and, separately, the reading each counter gave: the policy line
comes back as it was first stated, because a door names the ceilings it consulted and not
only the one that refused, and a field rebuilt from the readings alone would drop a
ceiling no reading was kept for; the quota comes back as the counter reported it; and
`RateLimit`'s `t` and `Retry-After` are worked out again at the moment the replay is
answered, so a closed window is never named again. The wait is the longest of the windows
that were shut: a counter with quota remaining did not refuse this answer. Those readings
are the whole of the replayed `RateLimit` line, one item per counter: the anonymous-write
guard spends a reading of its own on the way down, and a reply that kept both would state
one policy with two quotas and two resets. What is left of a window is what the row itself
had left when the event landed in it — the counter measures it against the clock at that
instant, not the instant its transaction opened, so a key several replicas are waiting on
costs a caller a shorter window than they were told and never a longer one.

A 303 replayed without its `Location` is a status with nothing to act on, so the
destination is part of the answer and not a header beside it. "The same write" is
digested over the method, the mounted
pattern, the path *as it arrived* — chi routes on `RawPath`, so `/records/a%2Fb/c` and
`/records/a/b%2Fc` are two resources and one decoded path is not one resource — the
query, the content type and the body. The same key with a different body is a 422 —
`IDEMPOTENCY_KEY_IN_USE` — because the only way out of a spent key is a fresh one; a
retry that arrives while the first is still running is a 409,
`IDEMPOTENCY_IN_FLIGHT`, which is the answer no caller can compute from the outside.
Records belong to the authenticated person, except at a `BearerKey` command:
there the key is the subject, so each key keeps its own retry record even when
one person holds both. Retiring one key cannot replay as another key's success.
Every unsafe write served by `kit/app` receives an `Operation-ID`: the parsed
key token, or a fresh UUID string when no key was supplied. `Operation-Location`
points an authenticated person to `GET /api/v1/app/operations/me/{ref}`. The
reference distinguishes uses of the same token on different routes or tenants.
Authentication, current route grants, tenant scope and the body ceiling precede
admission. A malformed key returns 400 without creating a receipt.

POST/PATCH on App/Public keep their existing answer replay. PUT/DELETE, Ops and
credential-minting routes still receive identity but store no replayable answer;
a keyed retry returns spent (409) and runs nothing. No credential or cookie is
kept for replay. An unkeyed retry is another intent, so clients needing retry
identity must persist their key before the first send, including offline queues.

| Attempt | Answer | The handler ran |
| --- | --- | --- |
| first with a key | whatever the route decided | yes |
| same scope/key and request bytes | stored answer, or 409 if spent | no |
| same scope/key with different bytes | 422 `IDEMPOTENCY_KEY_IN_USE` | no |
| same scope/key while in flight | 409 `IDEMPOTENCY_IN_FLIGHT` | no |
| no key | route answer and a newly assigned operation | yes |

The operation headers survive answer replay and replacement after rollback.
A definite refused execution records failed evidence after its domain transaction
rolls back; uncertain commits remain unknown. Mutation responses must fit the
existing buffer and cannot flush before commit. Synchronous writes stage success
with their authoritative transaction; an async owner calls
`request.DeferOperation(ctx)` and completes through the existing event delivery
transaction. Pending approval is recorded only by a committed proposal.

Answer replay lasts 24 hours from occurrence admission. An unresolved or
approval-pending identity survives that answer window and is then spent. A
terminal identity survives until at least 24 hours after its last outcome.
The five-minute lease preserves the operation reference on takeover. A begun
HTTP attempt is takeable only when its effect fence proves it did not commit.
`Options.Clock` supplies operation instants. [The receipt contract](../idempotency/README.md)
owns transitions, retention and person reads.

**An unanswered COMMIT.** A transaction can end without this process learning which way
it went: the COMMIT was refused, or it was sent and its answer never came back. Either way
the attempt records no answer and its claim stays begun. The claim writes a row into
`platformkit_command_effect` (migration `000072`) naming the attempt it hands the key to,
and every keyed command marks that row inside its own transaction (`idempotency.Store.Mark`),
so the mark is in the database exactly when the command's effect is. The press that
arrives after the lease reads the mark of the attempt the record names, across an
advisory lock the marking transaction holds until it ends: a lock still held means a
COMMIT is executing, answered 409 `IDEMPOTENCY_IN_FLIGHT`; a mark means the command
committed and its answer is gone, answered 409 `IDEMPOTENCY_ANSWER_NOT_RECORDED` and never
re-run; an existing unmarked fence permits an identical retry. Missing fences belong to
legacy or non-HTTP command roots and do not prove rollback. Taking the key
over re-names the row, which is the fence: an attempt still running past its lease finds
the row naming its retry, cannot mark it, and is rolled back. A mark that cannot be
written — that fence, or a store outage — rolls the command back and answers 503
`IDEMPOTENCY_UNAVAILABLE`, never a success without its mark. The row is the one here a
tenant transaction writes, under the shared tenant policy; a tenant can only mark its own
keys, which spends a key and never frees one.

A keyed upload is still bounded to `MaxBodyBytes` and returns 413 past that
ceiling; an unkeyed upload uses the configured upload ceiling. The guard restores
the body readers it fingerprints. Answer bodies larger than 256 KiB are spent,
while their minimal receipts remain readable. Standalone `httpx.New` callers
that omit `Options.Operations` retain the original optional replay behavior;
`kit/app` always wires the durable SQL owners.

**The fields.** A limited write answers with both fields, on the reply that was allowed
as much as the one that was refused: `RateLimit-Policy` is the field the draft asks to
stay consistent across a sequence of responses, and it costs no statement to write,
because it names the quota and window the guard was built with, and `RateLimit` carries
`r` and `t` as the counter reported them — for an allowed reply too, because a client
that hears the quota only when it is refused has already spent the traffic the field
exists to save. Both numbers come from the one statement that recorded the event. The window
that was refused also answers with `Retry-After`, so a client that only understands RFC
9110 still knows how long to wait. The policy is `public-writes`, one named constant
beside the window and the quota it describes — which were two literals in
[public_writes.go](public_writes.go) and are now one record with a name. The name, the
`q` and the `w` a client reads have to be the numbers the counter was keyed with, and
one sentence that quotes `60` three times is the only place they could ever have agreed.
What this delivery does *not* do is let a module invent a policy per route: a policy
name belongs to a counter, and a route that spends one states *that* counter's numbers
through `SayCounted` — which is how [modules/auth](../../modules/auth)'s own caps
(password reminders, link redemption) come to state the limit that decides them rather
than leaving the kernel's anonymous-write policy to speak for a counter it never
consulted — including the lockout, which is counted inside the sign-in command rather
than at its route and so states itself from there. Two counters on one reply is two items
in one list, in the order they were consulted; one is one. The port is one method wide, `Reserve`, which
answers the decision, the quota left and the window left from a single statement; it
replaced `Allow`+`Count` rather than sitting beside them, because two calls are two
readings of a row that had other writers between them. That widens `kit/limit`'s
published `Limiter` the same way — one method, same SQL, one more answer — and the break
is a deliberate one, taken before the release line froze its exported surface.

| Field | Carried on | Says |
| --- | --- | --- |
| `RateLimit-Policy` | every reply the limit applies to | the quota, the window, per policy |
| `RateLimit` | every reply the limit applies to | the quota left and the window left, per policy |
| `Retry-After` | every 429 | how long to wait, in seconds |

The counter's key is unchanged: tenant slug, method, the mounted route and
`ClientAddr(r)` — the peer address, and deliberately not `X-Forwarded-For`, which the
caller can write and which every office behind one egress shares. Two tenants behind
one egress are therefore counted apart, and a forged header buys an attacker nothing.
Both fields are rendered with quoted policy names and no commas
inside a list element, because RFC 9110 joins the two lists with commas, and a comma
inside an element would be a client reading one policy as two. The `request` scope
`kit/limit` already offers as a limiter for an unauthenticated request was read and
not used: it names the shape of a limit the auth module wanted, not the policy that
guards the anonymous write, and keying this counter by it would change what every
existing counter means.

A replayed answer restates the ceilings it was decided under. The policy line is
not rebuilt — the kept answer carries it verbatim, which is the only complete account
of it, and a door names every ceiling it consulted rather than only the one that
refused. The readings are the harder half, and are stored rather than rendered,
because what is left of a quota and of a window both shrink while a record sits in
one: `t` and `Retry-After` come from the window's own ending, and the quota is
restated as the counter reported it.

The line is written whole, over whatever a guard above left under that name: the
anonymous-write guard is spent before the memory is consulted, so a retry arrives with
a reading of its own already on the way down, from a counter this answer never
consulted. Kept beside the restated ones that is one policy with two quotas and two
resets — a client that reads `t=60` and then `t=0` has been told both that the window
is open and that it closed.

A refusal names the longest of the waits still running over the counters that were
shut when it was decided: an answer under two spent counters is not free to retry
until the later of them opens. A counter with quota remaining never shut this answer,
so its window does not belong in the wait — a refusal by a two-second cap inside an
open sixty-second one is a two-second refusal, and a caller told sixty sits out
fifty-eight seconds of a window that was never the reason they were refused. The open
counter's reading still goes on the `RateLimit` line, because the caller is under it;
only the wait counts the spent ones.

### Built on what came before

Decision 0022 asks a delivery to name what it composed rather than what it rebuilt.
**Reused:** the answer a replay carries is written by the same machinery every other
answer is written by — `respond`'s buffer, its `send`, and `writeProblem` as the one
encoder of the refusal shape — so a replay is an answer rather than a copy of one, and
inherits the rule that one status code is written once; the record is written from the
one defer that knows what the client was finally given, beside the refusal count that
already lives there, after `send` rather than before it; the memory store's claim, its
takeover of an abandoned claim and its verdict table are the shape `kit/limit`'s
`Allow` already answers with — one statement that decides and reports, and a database
which is not there is a refusal rather than an allowance; the purge rides `kit/jobs`
beside the limit sweep it belongs to; and `ClientAddr`, `WriteLimiter`, the body limit,
`problem`'s document,
`Public()` auth and `api.Recorded()` (the same list the catalogue walks) all appear here
in the shape they were written in. **Added:** `Options.Idempotency` and the
`idempotency` middleware — nothing keyed a write before, because the kernel answered a
second POST as a first; `limitPolicy`, the record that carries the quota, the window and
the name a caller reads, so the two fields cannot drift from the counter; and
`isOpsPath`, which the request-time chain and the declaration table now read together.
**Reused internally:** the verdict table behind `kit/idempotency`'s unexported `decide`,
which both the in-memory store and the Postgres store answer from, so one suite holds
both implementations — internal reuse inside that package, not a unit anybody imports, and
the same is true of `limitPolicy`'s encoder above; and
the `publicWrite*` constants, which name the limit that used to be two literals at its
one call site.

The marker that resolves an unanswered COMMIT: **Reused** — `kit/idempotency`'s `decide`,
`Store` and `Purge`, `kit/db`'s `Pending`, this package's `claimNote` and request
transaction, and the shared `platformkit_tenant_match` policy. **Added** — the
`platformkit_command_effect` table, `Store.Mark` and `Pending.Opened`, because the claim
row is written outside the command's transaction and so cannot say whether that
transaction committed. **Made reusable** — `Pending.Opened`, for any step that must write
a row inside the request transaction only when one was opened.


## The address a call was answered at

`ServedAuthority` is the address a call was answered at — the name its own `Host`
header named, on the port this process accepted the connection on — and the value a
mailed link is built from. It exists because a link has to be reachable by its reader:
a development installation serves every tenant at its name *and a port*, and a link
that keeps the name and drops the port opens a different server, or none. Only a call
knows the port it arrived on, and the worker that renders the mail runs long after the
call is gone, which is why the value travels in the event that call published.

The name is not for taking. A mailed link's name is its tenant's host of record, and a
call only reaches a tenant whose record already matches the name it asked at
(`tenant.go` resolves on `HostOnly`), so what this can contribute to a link is a port
on a name tenancy already vouched for.

The port is not for taking either, which is why it is read off the socket and not off
the header: `Host` is text the caller wrote. One request that reached a listener, aimed
at `acme.localhost:7`, used to be enough to make a victim's reset mail lead to port 7 —
a link carrying a live credential, addressed by the person best placed to phish for it.
The port a connection arrived on is the peer of that listener and cannot be chosen from
inside a request, so where the two disagree the socket is the truth.

A request that named no port contributes nothing even though its socket has one: naming
no port is how a browser says it reached the scheme's default, which is the case for
every installation fronted by a proxy that terminates on 443 and forwards to a port of
its own whose internal number must not become a public address. An installation whose
*public* port is neither the scheme's default nor the one its socket listens on — a
container published with `-p 38591:8080`, a compose `ports:` entry, a NodePort — is
answered by neither half: the header's port is a claim and the socket's is the private
one. Such an installation declares the public port in `server.public_host`, and the
application writes that port onto the host of record it hands its link builders, which
is then the one spelling a mailed link is built from without consulting any request
(see `servedPort` in modules/auth: a host that already spells a port is never rewritten
by the socket). What `ServedAuthority` contributes is the port of an installation that
declares no public one.

## The database boundary

This package imports `kit/db`, and the import is the contract rather than an
accident of layering. Three signatures name a transaction on purpose:
`TenantLoader.ByHost` takes the `db.Tx[db.System]` the kernel opens for the one
cross-tenant read a request makes, `Options.Authenticate` takes the request's own
`db.Tx[db.Tenant]` so a session lookup runs under row-level security, and
`TxFrom` hands a handler that same transaction — the only door to the database
a route has. `ConnFrom` and `WithConn` carry the `db.Conn` a control-plane route
opens a second transaction on, and the middleware chain owns the lazy
`db.Pending`: it opens on the first query, commits below 400 and rolls back
above, and closes early for the one route that streams its body.

Moving the pipeline into a `kit/db`-owned package would leave every one of
those signatures here, so the closure would not change; only the reader's path
would grow. The boundary is therefore drawn at the signatures: `kit/httpx`
imports `kit/db` for the transaction a request is, and imports nothing that
executes SQL of its own — resource registration reads `entity.Schema` and
`entity.Field`, and the single `kit/crud` value it names is `Query`, the page a
`Resource.List` is asked for. Presentation that needs neither lives in
[ui/document](../../ui/document/document.go) and
[ui/resource](../../ui/resource/resource.go); this package renders nothing.

Prerequisites: a `db.Conn`, a `TenantLoader`, an `Authorizer` and the
authenticate hook; [apps/platformkit](../../apps/platformkit/app.go) is the
composition to copy. Tests run against the development database: `make up`,
then `make test TEST_PACKAGES=./kit/httpx`.

## Composition of request steps

**Reused** — `New`, `Register`, `API.authorize`, the two admission paths and the
lazy request transaction remain the HTTP lifecycle. **Added** — private admission
and finalisation steps plus separate option, declaration, addressing and static
files make that lifecycle readable without adding a router or policy layer.
**Made reusable** — named steps for principal/grant checks, body restoration,
receipt reservation and commit preparation within the existing request owner.

[Construction](httpx.go) retains the middleware order, with inputs in
[options.go](options.go) and the boot gate in [declarations.go](declarations.go).
[Surfaces](surfaces.go), [router paths](router.go), [mounted routes](mounted_routes.go)
and [static trees](static.go) retain their existing addresses and recording.
[Authorization](authorize.go) precedes [legacy keyed admission](idempotency.go) or
[receipt admission](operation_receipt.go); their clocks, contexts and responses
remain distinct. [Transaction finalisation](transaction.go) prepares the receipt
and effect marker before the single ordinary close, then chooses the response.
