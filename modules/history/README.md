# History module

`modules/history` is every version of a record's content, kept by the write that
made it, and the retention rule that decides how many of them stand. It stores no
record and mounts no screen: it is the store a record's own address answers
`GET {item}/versions` from, and the store a restore reads one version out of. A
restore writes a new version with the stored content and rewrites nothing, so the
series only ever grows and "what was here before" stays answerable after a
restore, a sweep and a mistake.

The version number is the row's own write count (`entity.RevisionField`), read
after the UPDATE that moved it. There is no second counter here, and
`rest.Spec` refuses a `History` Spec whose entity carries none, because a version
number nobody can quote against a row is a number with no meaning. A series can
skip numbers — a write that moves lifecycle rather than content moves the count
and keeps no version — which is why the table is keyed by the number rather than
running unbroken.

## Composition

The reference application names it in `Use` (`apps/platformkit/app.go`), between
the file module whose ledger it writes through and everything that needs a
version kept. A resource takes the port, not this module:

```go
// httpx is github.com/septagon-oss/platformkit/kit/httpx
rest.Spec[*contracts.Content]{Module: "content", Entity: "content", …,
	Versions: deps.Versions, History: true,
}
```

`content.Module` declares `pkit.Needs[httpx.Versions]()` for it and turns
`History` on for its own Spec, so pages are the reference application's versioned
records. `history.Module` needs `rest.FileUses` — the file module's ledger — and
`jobs.TenantLister`, which the tenant module provides; it provides
`contracts.Service` and `httpx.Versions`, the one the resource records through.
`history.New` is the constructor behind that: `Deps{Uses, Tenants, Retention}`,
with `Retention` zero for `Defaults` (ninety days or the newest fifty versions,
whichever keeps more). A rule that keeps nothing — a negative on either side — is
refused where the plan is built, not at the sweep's first hour.

Two halves of the feature are not in this module, and neither is a detail: the
recording and the restore are `kit/rest`'s, because the guard, the surface, the
tenant transaction, the schema and the record's address all belong to the record
(`kit/rest/history.go`); and `history.restored` is declared typed here
(`contracts.Events`) because a composition that mounts a versioned resource and no
keeper of versions is a boot refusal rather than an event nobody answers.

## What is stored, and what is filed

One table, `history_versions`, owner `"history"` (`migrations/000055_history.up.sql`):
the record's address (module, entity, record id, locale, version), the document as
that write committed it, the field names it moved, the write's own sentence about
why (`''` for an ordinary save, `restore:<n>` for a restore), who made it and when.
The `UNIQUE` on `(tenant, module, entity, record, locale, version)` is what makes
"one version per committed write" the database's rule rather than this module's
promise.

The files a stored body shows are filed under the version that shows them, through
the same `rest.FileUses` ledger the body itself is filed under, and the list is
kept in the row. So the sweep releases exactly what the versions it removed were
holding alive and nothing the versions it keeps also show — a body is read for its
references once, by the door that parsed it, and nothing re-parses a stored body at
sweep time.

## Retention

`history-retention`, hourly, `jobs.PerTenant`, one tenant per transaction so that
row-level security is the boundary inside the job exactly as it is inside a
request. It removes the versions `Rule.Keep` does not keep — rank within the
series and the committed instant, nothing else — and releases the files they were
keeping alive. It publishes nothing: an expiry that announced itself would be
relayed into the table the relay is trimming. With no `TenantLister` wired the job
refuses rather than letting versions stand forever as if that were the plan.

## Limits — the module's share

- How much history stands is the module's default and the composition's sentence,
  never a sector's number: ninety days or the newest fifty, whichever keeps more,
  and zero on either side is the composition saying nothing. A negative is a
  mis-set setting and is refused at boot.
- A restore replays the writable fields only. A stored document is the whole row,
  and the row's lifecycle, identity and write count belong to the doors that own
  them: a restore that replayed them would unpublish a page or forge a byline.
- `expectedRevision` is a condition, not a value. A quote the row is past answers
  412 and writes, publishes and returns nothing. A restore that quotes no revision
  is last-write-wins, exactly as a PATCH without `If-Match` is: a panel or a phone
  sends the revision it read, and the door rechecks it under the row lock either
  way.
- A delete records no version: it moves a flag, not content, and the last content
  version stays as the record's trail.
- A write that never comes through `rest.Spec` records no version. A lifecycle
  command, a seed or a job moves the row's count without keeping a document, which
  is why the first version of a seeded page is the first author's save.
- The versions themselves have no versions: no diff, no annotation and no
  per-version permission lives here.

## Authorization

### Permissions

None. `module.go` declares no permission and no `Nav` entry, and mounts no route:
its `Permissions`, `Nav` and `Routes` are all nil. A version of a record is read at
that record's own address under that record's own read permission, and restored
under its write permission (`kit/rest/history.go` mounts both through the record's
`Spec`), so a permission declared here would be a second key for a door that
already has one — and a key with nothing behind it is a grant that does nothing.

### Object scope

Tenant, and within it the record the caller may already read. Row-level security is
the whole boundary: `ENABLE` and `FORCE ROW LEVEL SECURITY` with
`platformkit_tenant_match(tenant_id)` on both `USING` and `WITH CHECK`, so another
tenant's version is not readable and not forgeable even by a store call that
skipped a check. The tenant is never an argument to any method — it is the
transaction's — and no per-row rule narrows a series further, because "which
records may this caller read" is the record module's question and this module
answers only "which versions of this record does this tenant hold".

### Duties the module enforces itself

Writing the same number twice is refused (`ErrDuplicateNumber`, and the unique
constraint saying so if the check loses the race), and nothing is written by the
refusal. A version whose number is already taken means a caller that counted twice
or raced itself, and quietly replacing the stored document would be the loss rule 8
refuses. The sweep removes only rows of its own tenant and releases only the files
the removed rows named, in the transaction that removes them; a `Prune` that
deleted without releasing would strand a file nothing reads, and one that released
what a kept version also shows would delete a picture a person can still open.
`Rule.Fault` refuses a rule that keeps nothing before the sweep can run it. The
restore's own duties — the actor's grant, the tenant scope, the revision recheck —
run in the record's write transaction, in `kit/rest`, beside the row lock.

### Public faces

None. There is no public route, no `Face`, no anonymous read and no generated
screen of this module's own: a version series is read where the record it describes
is read, and the record's visibility rules are the series' visibility rules. An
author's private draft and a published page's history are both reachable only
through the record's read permission.

### The operator boundary

Nothing here is the operator's. The module declares no operator permission and
mounts nothing on the operations surface; the retention job runs for every tenant
the `jobs.TenantLister` answers with, on the schedule the composition declared, and
takes no instruction from a request. An operator who can read a record reads its
versions through the same address and the same grant as everyone else — there is no
side of the product where a version series is visible without the record.

### Provisioning

Nothing to provision per tenant: the table arrives from the migration, and a
tenant's first version is the first write that keeps one. The retention rule is the
only setting, it is the composition's rather than a tenant's, and the loader that
does not read a rule leaves the module on `Defaults` — which is why no key exists
for it here: a setting nothing reads is a setting nobody set.

## Reused, added, made reusable

**Reused** — `rest.FileUses` for the ledger a version's pictures are filed in,
`rest.Command` and `rest.Spec`'s stale-tag rule for the restore (no new door),
`jobs.Job` with `jobs.PerTenant` for the sweep, and `dbtest.Schema` with
`platformkit_tenant_match` in the migration and its tests.
**Added** — the version table and its store, the retention rule and the sweep,
because nothing in the kernel or the reference modules kept a superseded document,
and a diff or a restore needs one.
**Made reusable** — `httpx.Versions`, the port a resource records through, declared
where its two readers can both reach it (`kit/rest` writes a version inside the
write; a screen lists them), and `history.restored` as a declared typed event, so
another module can subscribe to "a version came back" without importing this one.
