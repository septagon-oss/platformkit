# 6. System access is a token handed to a module at wiring time

Status: accepted, 2026-09-02

## Context

ADR 0003 makes cross-tenant access a type: `db.Tx[db.System]` cannot be produced
outside `kit/db`, and `db.RunSystem` needs a `tenancy.SystemToken` that only
`kit/internal/syscap` can mint. Until E3 that was enough, because the only
callers were kernel ones — host resolution, the outbox relay, the periodic job
that walks the tenants.

E3 breaks the assumption, and it breaks it for a good reason rather than a
convenient one. A tenant is a row that belongs to no tenant. Creating one,
listing them, suspending one: none of that can happen inside a tenant
transaction, because the tenant is what is being created, listed or stopped. So
a *module* now needs the capability, and the question is which door it comes
through.

The answers we rejected:

- **Export the minting function.** `syscap.NewSystemToken` from anywhere makes
  the capability free, and a capability everyone can mint is not a capability.
- **A field in `Deps`, minted in `main`.** This is the shape we wanted, because
  `grep SystemToken apps/` would then be the whole list. It does not work:
  `main` composes modules before `kit/app` opens anything, so `main` has nothing
  to mint from and nothing to mint with.
- **A kernel-mounted "system route" kind.** A second registration path beside
  `httpx.Register` is a second thing to keep honest, and a route that skipped
  the ordinary authorization would be a door that satisfies the boot gate and
  evades the enforcement — the failure `kit/httpx`'s package comment already
  describes.

## Decision

The kernel hands the capability to a module at the one moment it is being
wired: `Module.Routes` receives the `*httpx.API`, and `(*API).SystemToken()`
returns a token there.

    Routes: func(api *httpx.API) { internal.RegisterRoutes(api, svc, api.SystemToken()) }

Three things follow, and each is a property rather than a convention.

1. **The set of modules that cross tenants is one grep.** `grep -rn
   'SystemToken()' modules/` lists them, next to the manifest a reviewer is
   already reading. Today it is one line, in `modules/tenant`.
2. **A handler holds no ambient authority.** The token is obtained where a
   reviewer sees it — in `Module.Routes`, next to the manifest — and closed over
   by the handlers that need it. `(*API).SystemToken()` is a method on a value
   the kernel passes to `Routes`, so the door is the wiring moment and the grep
   is the whole list. It is not a claim that the capability is unreachable
   afterwards: a module that kept the `*httpx.API` in a field could call the
   method later, and the first version of this ADR said "cannot acquire it
   later", which was an overclaim. What is true is the property this design was
   chosen for — every module that crosses tenants writes one line where the
   manifest is read.
3. **The transaction is a second one, and says so.** A request that reached a
   control-plane route has already opened its tenant transaction — recognising
   the caller was a query in it — and `db.RunSystem` refuses to widen a tenant
   transaction into a system one. `db.Detached` is the call that says a new
   transaction is being opened, and its documentation states the consequence:
   the control-plane write commits on its own, so it survives a request that
   afterwards fails.

Two smaller doors come with it, and both exist because `db.RunSystem` takes a
connection as well as a capability: `httpx.ConnFrom` is the request's
connection, and `events.PublishFor` is the one place in the program where the
tenant an event belongs to is an argument rather than a property of the
transaction — because the event that says a tenant was created is written in the
transaction that created it.

`kit/app.Bootstrap` is the same decision at the other end of the life cycle: an
installation with no tenants has no tenant transaction to do its first write in,
so the kernel opens a system one and hands it to `platformkit bootstrap`.

## The token names the module that holds it

The grep was a list of callers and not a description of the work. Four modules
take a token in this repository, and every cross-tenant transaction they open
was logged with one shared sentence — "a module's control-plane routes" — which
cannot be read afterwards: it does not distinguish the file sweep from the tenant
directory. So the need is now declared, in the manifest beside the call:

    SystemAccess: module.SystemAccess{Reason: "sweep stored bytes no row references, across every tenant"}
    Routes:       func(s httpx.Surfaces) { sweep.Use(s.Ops.SystemToken()) }

Four things follow.

1. **The set of modules that cross tenants is one field and one sentence each.**
   `grep -rn 'SystemAccess:' modules/` names them and says why; the audit trail
   of every `db.RunSystem` now carries `module` beside `reason`, with an empty
   `module` saying the kernel opened it — host resolution, the relay, a probe.
2. **A token taken without a declaration is refused at composition**, naming the
   module and the field to add, and the minted credential is `nil` as well, so
   the capability is not handed out on the strength of a call nobody declared.
   `httpx.API.ValidateDeclarations` answers it, which `kit/app` and `pkit` ask
   before any port opens.
3. **A declaration nothing took is refused too.** The mint marks each credential
   it hands out, and `(*API).untakenDeclarations` reads that mark against the
   declarations — a module that declared the need and never asked is a manifest
   nobody ran through `Routes`. `httpx.API.ValidateDeclarations` answers it, so
   the question is asked after every module's `Routes` has run, which is when
   `kit/app` and `pkit` ask it. A module that stops crossing tenants
   and leaves the line behind is a manifest that no longer describes the code,
   and the job written beside it is running on nothing.
4. **One credential per module, answered the same on every registration pass**,
   because kit/app registers a composition's routes four times and compares them.

The reason is bounded to one line and one sentence by `module.Validate`, because
it lands in a `slog` field and a span attribute and nowhere else: a bound nothing
enforces lets the audit line become a paragraph. It is operator text in the
source language and deliberately not translatable — a security fact a tenant
could rewrite in a catalogue is not a security fact.

The shape we rejected besides the three above:

- **A `pkit.Needs[...]` read, or a `pkit.SystemAccess(reason)` declaration.**
  Nothing can answer it where it would be answered: `pkit` calls `kit/app.New`
  and `httpx.New` inside it, so during a module's build there is no API to mint
  from, and `Wiring.refuse` would refuse a module that did everything right.
  A pkit-only declaration would also be invisible to a composition built through
  `kit/app.New` directly, which is public and used by every app test. The
  manifest travels with the module, and one place holds the one fact.
- **A mint on demand, from a reason the call site passes.** Then the sentence in
  the audit trail is written by whoever wrote the handler, and the manifest is
  decoration. The kernel's mint takes no reason as an argument at all: it reads
  the declared one, so the sentence exists in exactly one place.

`(*API).SystemToken()` — the one shared token, with no module on it — stays,
deprecated, until the next major. What the deprecation costs here is nothing: no
package under `kit/`, `pkit/` or `apps/` calls it after this change, and the
module-facing doors (`Surfaces.SystemToken`, `Router.SystemToken`) answer for the
module that received them. A router borrowed into another module's namespace —
the namespace a caller holds as the owner's `tenancy.Namespace`, asked of
`Router.Known` and `Router.ForModule` by its `Module()` — is refused a token and
says so naming both modules: a page mounted beside somebody else's screens is
still this module's page, and it borrows nobody's credential.

## The capability is not the authorization

The token says a module may open a cross-tenant transaction. It says nothing
about who may make the request that reaches the handler holding it, and the two
were confused once, in this repository, with a real consequence.

The control plane is served on every tenant's host, because an installation has
no host of its own — only its customers'. So `POST /api/v1/tenant/tenants` is
reachable at `acme.example.com` and at `globex.example.com` alike, and what was
supposed to keep it safe was the permission `tenant:manage`. It did not: a
permission is a string in a tenant's own `roles` table, and every tenant's
`admin` role holds `'*'` by construction. E3.1's review signed in as a second
customer's administrator and listed, created and suspended tenants.

So a route may now declare `httpx.OperatorPermission` instead of
`httpx.Permission`, and the permission is declared `Operator: true` in the
manifest that defines it. Three things follow, and the first is the one that
closes the hole:

1. **The kernel refuses the request before it asks anything.** `tenancy.Tenant`
   carries `Operator`, one row in `tenants` has it — the one `platformkit
   bootstrap` created — and the authorize middleware answers 403 to an operator
   grant on any other tenant. No roles table is consulted, so no way of writing
   a role changes the answer.
2. **The wildcard stops answering for it.** `auth.Grants` lets `'*'` satisfy an
   ordinary permission and never an operator one: the role has to name it.
   `'*'` means "everything in this tenant", and the control plane is not in this
   tenant.
3. **The two declarations have to agree.** `kit/app` refuses to start when a
   route and the manifest that defines its permission disagree about the kind,
   naming both. A control-plane route that declared the ordinary kind is
   indistinguishable from a working one until somebody tries it from the wrong
   tenant, which is precisely how this was missed.

The `Operator` column is written by the bootstrap and by nothing else.
`NewTenant.Operator` is `json:"-"`, so the create route has no field to fill in
however the body is written, and a unique partial index says there is at most
one.

## Consequences

- The tenant module's five routes are hand-written rather than a `rest.Spec`,
  because a `crud.Entity` carries a `tenant_id` and a tenant does not. That is
  what the exception costs, and it is five short handlers; the errors are still
  `kit/crud`'s, through `crud.Classify`, so a 404 means the same thing here.
- `tenants` and `tenant_hosts` are declared exempt from tenant scoping, in the
  comment the convention uses, and they are not unprotected: the policy lets a
  system transaction see everything and an ordinary tenant transaction see
  exactly one row, its own. `TestATenantTransactionSeesOnlyItsOwnRow` is that
  claim.
- A control-plane write inside a failing request is kept. This is the one place
  in the application where that is true, and it is deliberate: the alternative
  is a tenant that exists in one table and not another.
- The gate is a grep and a review, not a privilege — the same shape as
  `scripts/check_gucs.sh`, and for the same reason ADR 0003 gives.

## Evidence

```sh
grep -rn 'SystemAccess:' modules/                # every cross-tenant module, and its reason
grep -rn 'SystemToken()' modules/ apps/          # where each one takes its credential
go test ./kit/app/ -run 'TestAModuleThatTookACrossTenantCredentialItNeverDeclaredIsRefused'
go test ./apps/platformkit -run 'TestTheModulesThatCrossTenantsNameFourDifferentReasons'
go test ./modules/tenant/... -run 'TestATenantTransactionSeesOnlyItsOwnRow'
go test ./modules/auth/... -run 'TestASessionFromAnotherTenantIsNotASessionHere'
```
