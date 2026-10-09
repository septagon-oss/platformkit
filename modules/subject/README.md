# Subject module

A person's two doors: the copy of everything the installation holds about them, and
the request to stop holding it.

This module owns no person's rows. Each owner of a person's rows — `user`, `auth`,
`notification`, `file`, `task`, `content`, `change`, `audit` — contributes a
`personaldata.Part`, and this module asks each one, in module-name order, for its
export and its erasure. It is the only place in the program that can say *this
person, everywhere*, because house rule 3 forbids any one owner reading another's
tables ([ADR 0020 §1](../../docs/adr/0020-a-persons-data-export-and-erasure.md)).

What it does own is the *request*: the row that says a person asked, who asked it, what
came of it, and the one-time link that lets them open the answer. ADR 0020 §5 and §6
decided that shape; `migrations/000046_subject_requests.up.sql` is its bookkeeping.

* **Reused**: `kit/personaldata` (the contract, the `Subject`, the scrub every owner
  shares), `pkit.Needs[[]personaldata.Part]` and `pkit.All` for the collection of
  contributions, `usercontracts.Service` for the one read across the boundary,
  `kit/events` for the outbox write, `kit/crud`'s refusals and `crud.Classify`,
  `kit/jobs.PerTenant` for the walk, `file.Service` for a bundle's bytes and its
  retention hold, `notification.Service` for the person's inbox.
* **Added**: the composition of the parts into one answer, the zip bundle and its
  manifest, the second confirmation checked against the row, the receipt that a
  repeated erasure answers instead of re-running, the request row and its state
  machine, the export walk, and the seven-day single-use hashed link.
* **Made reusable for anyone else**: a module that must ask several owners the same
  question of one entity, and cannot read any of them. The parts pattern is the
  answer, and `modules/subject` is its worked example — contribute what you own of
  the answer, and let the one module that composes them own the order and the shape.
  The second is the `BundleSource`/`Notifier` pair in `contracts/requests.go`: a module
  that needs a blob and an inbox takes two methods, and the composition writes the
  four-line adapter rather than the module importing a storage SDK.

## Composition

`pkit.Needs[[]personaldata.Part]()`, `pkit.Needs[usercontracts.Service]()`,
`pkit.Needs[filecontracts.Service]()`, `pkit.Needs[notificationcontracts.Service]()`,
`pkit.Needs[jobs.TenantLister]()`, `pkit.Provides[subjectcontracts.Service]()`. The
resolver builds this module after every contributor, which is the ordering that is
wanted and the reason it does not say `After(Everything)` — that declaration also
refuses anything the module provides.

The reference application composes it between `change.Module` and `admin.Module`
(`apps/platformkit/app.go`), which is what turns the eight contributions into a
command rather than a manifest claim.

## The two operations

`Export(ctx, tx, subject)` is a read. It answers with `manifest.json` and one
`<module>.json` per part in a stored-method zip with zero modification times, so two
exports of the same rows are the same bytes and a bundle can be digested and
compared. The manifest names the tenant read off the transaction, the person, each
part's size and sha256, and `leftOut`: what the bundle does not carry is said there,
so the answer to *what is missing* is inside the bundle. One contributor failing
fails the whole export — six parts and a silent gap read exactly like a person with
nothing in them.

`Erase(ctx, tx, {subject, confirm})` asks the person's own module first and the rest
second. The confirmation is the address, repeated, checked against the row before
anything is written. A person already carrying the replacement answers the first
erasure's receipt and is not scrubbed again. The owners run in the caller's
transaction, so an owner that refuses stops the list and publishes nothing; the
commit that scrubs the rows also writes `subject.erased`, which is how an
irreversible act reaches the audit trail.

## The doors

`Export` and `Erase` are commands; the door is what turns one into something a person
can ask for from a browser. The sequence is the ADR's, and every step of it is a row:

| step | what happens | where |
| --- | --- | --- |
| ask, and ask again | one `subject_requests` row in `requested`, and `subject.export_requested`, in one transaction; the row read back by whoever filed it, which is the address the notice links to | `POST /api/v1/subject/exports` with no permission (the person's own), or `…/exports/for` with `subject:export`; `GET /api/v1/subject/exports/{id}` |
| work | the `subject-export` job walks each tenant's open requests through `jobs.PerTenant`, calls `Export`, stores the zip private through `file.Service`, holds it, files the row `ready` and notifies the requester | `internal/requests.go`, one transaction per request; a failure files `failed` with the reason in `detail` and publishes nothing |
| open | the requester mints a token — `POST /api/v1/subject/exports/{id}/link` — seven days, single use, stored as a sha256 in `subject_grants`, and the hold on the bytes moves out to the link's own expiry | `Mint`; `contracts.Link.Token` is the only place the token exists outside that response |
| download | `GET /api/v1/public/subject/bundles/{token}` streams the bundle out of the file module and spends the token in the same statement that reads it | `Redeem`; used, forged and expired answer the same 404 |
| erase | `POST /api/v1/subject/erasures` with `subject:erase` runs the erasure and files its receipt in the caller's transaction; a refused erasure writes nothing, including the row | `RequestErase`; the unique index on `(tenant, subject)` for `kind = 'erase'` is what makes the request that arrives twice answer the first one |

Nothing here is a `rest.Spec`, and that is modules/audit's reason restated: a Spec is
five routes and three of them write. A request row only ever moves forward, and a PATCH
that could set `state` would be an API in which anyone can mark an erasure as done.

## Authorization

Decision 0011's six questions.

### Permissions

Two, declared in `module.go` and labelled in `messages/pt-PT.json` so a refusal names the
grant in the language the tenant is served in:

* `subject:export` — produce a copy of *anybody's* rows, and mint a link onto one the
  caller did not request.
* `subject:erase` — stop holding a person's data, irreversibly.

The person's own export declares neither: it declares `httpx.SignedIn` and takes the
subject from the session, because Article 15 is a right and cannot require a grant a
person might not hold. That route has no `subject` field in its body for the same reason:
a body that could name somebody else would be a door onto their rows under an
authorization that asks for none.

`subject_officer` is the one role this module declares, granting exactly those two. Which
of a product's roles may erase is the product's `SeedRoles` line (ADR 0020, Limits); the
reference application seeds the officer in `apps/platformkit/roles.go`, and
`TestEveryRoleEveryComposedModuleDeclaresIsOneThisApplicationSeeds` is what keeps the two
one answer.

### Object scope

The tenant, and the person inside it. Every command takes a `db.Tx[db.Tenant]`, so
row-level security is the bound and no query here names a `tenant_id` in its `WHERE`: the
policy `platformkit_tenant_match` is what makes one tenant's request row absent to
another, and `TestOneTenantsRequestIsNotAnotherTenantsRow` asks exactly that (a second
tenant reads nothing, mints nothing, and the walk over its own tenant leaves the first
tenant's row where it was). A second check is per-object and it is this one: a mint is
answered only to the requester or to a holder of `subject:export`, which the route states
and the command rechecks against `requested_by`, because a request id is not a capability.

### Duties the module enforces itself

Five, and they are the ones no owner can see from inside its own table. The confirmation
must be the address the row holds, or nothing is written (`crud.ErrInvalid`). A person
already carrying the replacement is not scrubbed a second time. Every owner is asked in
module-name order, so one person's export does not depend on the order the resolver
happened to build the modules in. A request row is stamped with the tenant of the
transaction that wrote it and never with one the caller named. And a mint asked by
somebody who did not file the request answers `crud.ErrNotFound` rather than
forbidden — the answer that does not confirm the row exists. What it does not enforce is
what each row becomes — whether a task keeps its assignee, whether a published page
survives: that belongs to the owner of the row and is asked of it, not decided from here.

### Public faces

One: the bundle link, on the public surface, because the person who asked for a copy has
to be able to open it at three in the morning on a phone with an expired session. It is a
token door and nothing else is mounted there — no list, no page, no request read. No nav
entry and no generated screen: see Limits.

### The operator boundary

`subject:erase` is an ordinary tenant permission, not an operator one, and that is a
decision rather than an oversight: an erasure is a person's right answered inside their
own tenant, and an installation's ability to erase anybody in any tenant would be a
control-plane door over eight modules' rows at once. Nothing in this module is mounted on
`r.Ops`. What the operator does own is the *deployment's* copy of the bundle's bytes,
through the file module's own retention rules — which is why the hold travels with the
link and is placed by this module rather than assumed.

### Provisioning

Two permissions, one role, one migration (owner `subject`, version 46, the next number
past the highest in the composition). No configuration key and no feature flag: the doors
are the module. The bootstrap that creates a tenant does not grant either permission to
the administrator beyond the tenant's own `admin` role, which holds everything the tenant
owns — so a tenant that does not want its administrators erasing people revokes it, and
that revocation is the product's, not this module's.

## Limits

* **The bundle's ceiling is not implemented.** ADR 0020 §5 sets `MaxBundleBytes` at 64 MiB
  and says that below it a file's *bytes* are attached to the bundle, and above it the
  manifest names each file whose bytes it left out. Today the file module's part carries
  metadata only — `modules/file/internal/personaldata.go` answers name, content type, size,
  digest and dates, and its own `leftOut` list exists for the ceiling and is never filled —
  so a person's uploaded bytes are not in their export at all. `Manifest.LeftOut` here is
  the same promise at the bundle's own level and is likewise empty until the ceiling is
  built. That is a gap against the ADR, it is one the file module owns, and it is the next
  thing here.
* **No page, no `rest.Spec`, no nav.** The request rows are readable through the API and
  the person's notification points at the request, not at a screen. `internal/ui` renders
  nothing, so the generated screens and the operator's desk are owed with the page that
  reads them.
* **The link reaches the person in-app.** The notice says the bundle is ready and points at
  the request; the token is minted by the person from the link route and is never written
  into a row, an event or a log — which is also why it is not in an email. Mailing it is
  the product's `Mailer`, its copy and its host (ADR 0020, Limits).
* **The erasure runs in the caller's transaction.** It is the slowest command in the
  kernel, and ADR 0020 says a large tenant should run it asynchronously through the same
  `requested → running → erased|failed` machine the export uses. The row and the unique
  index are already the shape that needs; the walk over the `erase` kind is not written, so
  today an erasure of a big tenant is one long transaction and a client that gave up
  waiting is a client that will retry, get the same row, and be refused.
* **This module files its own tables and contributes no `Part`.** `subject_requests` and
  `subject_grants` name a person and are declared (`module.go`), but they are not scrubbed
  when that person is erased: the row that records an erasure has to outlive it, and the row
  that records an export request is the receipt for a copy the person was owed. The ADR's
  §8 row for the kernel's outbox is answered by nobody yet, for the same reason it names:
  the orchestrator would have to read tables the kernel owns.
* **Per-owner coverage is uneven.** `modules/file` and `modules/notification` implement
  `personaldata.Part`, are read by the acceptance line in
  `apps/platformkit/exporting_a_seeded_person_finds_their_rows_from_each_module_test.go`,
  and ship no seeded personal-data test of their own; the other six owners do.

The manifest claim is `personaldata.Declaration`'s presence — two tables, four uuid
columns that name something, said out loud so the boot rule can check the sentence against
the SQL — and the acceptance line that a person's rows come back from every owner and stop
naming them is proved by
`apps/platformkit/exporting_a_seeded_person_finds_their_rows_from_each_module_test.go`,
which is an application test because no module may read across the boundary the test has
to cross.
