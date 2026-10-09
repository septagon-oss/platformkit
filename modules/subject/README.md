# Subject module

A person's two doors: the copy of everything the installation holds about them, and
the request to stop holding it.

This module owns no rows. Each owner of a person's rows — `user`, `auth`,
`notification`, `file`, `task`, `content`, `change`, `audit` — contributes a
`personaldata.Part`, and this module asks each one, in module-name order, for its
export and its erasure. It is the only place in the program that can say *this
person, everywhere*, because house rule 3 forbids any one owner reading another's
tables ([ADR 0020 §1](../../docs/adr/0020-a-persons-data-export-and-erasure.md)).

* **Reused**: `kit/personaldata` (the contract, the `Subject`, the scrub every owner
  shares), `pkit.Needs[[]personaldata.Part]` and `pkit.All` for the collection of
  contributions, `usercontracts.Service` for the one read across the boundary,
  `kit/events` for the outbox write, `kit/crud`'s refusals.
* **Added**: the composition of the parts into one answer, the zip bundle and its
  manifest, the second confirmation checked against the row, and the receipt that a
  repeated erasure answers instead of re-running.
* **Made reusable for anyone else**: a module that must ask several owners the same
  question of one entity, and cannot read any of them. The parts pattern is the
  answer, and `modules/subject` is its worked example — contribute what you own of
  the answer, and let the one module that composes them own the order and the shape.

## Composition

`pkit.Needs[[]personaldata.Part]()`, `pkit.Needs[usercontracts.Service]()`,
`pkit.Provides[subjectcontracts.Service]()`. The resolver builds this module after
every contributor, which is the ordering that is wanted and the reason it does not
say `After(Everything)` — that declaration also refuses anything the module provides.

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
anything is written. A person whose row already carries the replacement answers the
first erasure's receipt and is not scrubbed again. The owners run in the caller's
transaction, so an owner that refuses stops the list and publishes nothing; the
commit that scrubs the rows also writes `subject.erased`, which is how an
irreversible act reaches the audit trail.

## Authorization

Decision 0011's six questions, answered. The short answer to the first is that this
module declares none yet, and why it declares none is the honest part of the answer.

### Permissions

None. The two operations are commands, not routes, and this module mounts nothing:
`subject:export` and `subject:erase` are ADR 0020 §7's, and they arrive with the
`rest.Spec` and the two doors that need guarding. A permission key no route checks
is a box an administrator can tick forever with no effect, and a field nothing reads
is not written, so the declaration waits for the door that reads it. When the door
lands, the person's own export is `/me`-shaped and asks no permission at all —
Article 15 is the person's right and cannot require a grant a person might not hold.

### Object scope

The tenant, and the person inside it. Both commands take a `db.Tx[db.Tenant]`, so
row-level security is the bound and no query here names a `tenant_id`; the tenant a
bundle names is read off the transaction rather than handed in. There is no
per-object policy to consult, because the module holds no object: everything it
returns belongs to rows whose owner applies its own scope.

### Duties the module enforces itself

Three, and they are the ones no owner can see from inside its own table. The
confirmation must be the address the row holds, or nothing is written
(`crud.ErrInvalid`). A person already carrying the replacement is not scrubbed a
second time. Every owner is asked in module-name order, so one person's export does
not depend on the order the resolver happened to build the modules in. What it does
not enforce is what each row becomes — whether a task keeps its assignee, whether a
published page survives: that belongs to the owner of the row and is asked of it, not
decided from here.

### Public faces

None. No page, no route, no `rest.Spec`, no link. The seven-day bundle link of ADR
0020 §6 is a public face and does not exist yet; until it does, nothing in this
module answers a request from a browser.

### The operator boundary

Nothing here is the operator's alone, because nothing here is reachable. When the
doors exist, `subject:erase` is the one the composition will want to hold tightly —
it is irreversible, and the person's own right to ask for it is the reason the
command cannot simply be an operator's tool. That decision is the door's and is
written in the README that ships with it.

### Provisioning

Nothing. No role, no permission, no configuration key, no table. The bootstrap that
creates a tenant and its first administrator does not touch this module, and a
deployment that composes it gets the same two commands and no new state to migrate.

## Limits

The door is missing: no route, no permission, no `subject_requests` or
`subject_grants` table, no export job, no bundle stored through `file.Service` with
its retention hold, no minted single-use link, no notification that a bundle is
ready, no `subject-officer` role. The commands are composed and callable from the
plan today; the acceptance line that a person's rows come back from every owner and
stop naming them is proved by
`apps/platformkit/exporting_a_seeded_person_finds_their_rows_from_each_module_test.go`,
which is an application test because no module may read across the boundary the test
has to cross.

The manifest claim is `personaldata.Declaration`'s absence: the boot rule asks a
module what its own tables say about a person, and this one creates no table. That is
also why it contributes no `Part` of its own — ADR 0020 §8 has the orchestrator
answering for the kernel's outbox rows, and it cannot do that until it owns the SQL
that reads them.
