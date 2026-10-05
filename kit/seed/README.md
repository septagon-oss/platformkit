# Embedded application seeds

`kit/seed` loads embedded YAML or JSON documents, orders declared references,
and compares the requested values with owner snapshots. An application supplies
an explicit `Writer` for each resource; that writer calls its owner's normal
create, update, command and delete paths. `Service.Plan` is read-only;
`Service.Apply` operates within a caller-owned `db.Tx[db.Tenant]` and uses a
per-tenant advisory transaction lock. An error must be returned to the caller
so the enclosing transaction rolls back.

## Composition

**Reused:** `db.Tx[db.Tenant]`, `db.InTenant`, `Load`, `Order`, `Decide`, and each
resource owner's write path supply transaction scope, source parsing, graph order,
canonical comparison and domain validation. A page is created, patched and deleted
through `rest.Spec`'s write core — the body its JSON route runs, exported so that a
seed cannot grow a second spelling of it — and its lifecycle moves through
`content.Service.Publish`. A person is created through the user module's own
invitation. `auth.Service.Permissions` is what the grant check reads, and the
actor's own row, re-read through `crud` in the run's transaction at the moment it
asks, is where it reads the roles from.

**Added:** `seed_keys` under database RLS and an explicit `Writer` service were
needed because no existing table records seed provenance and no existing kit
operation coordinates several owner writes in one selected run. `app.RunCommand`
opens the cross-tenant transaction an operator command needs: the system token is
internal to the kernel, so a composition cannot mint one for itself.

**Made reusable:** `Writer`, `Authorizer`, `Plan` and `Apply` let another app
declare its own resources and embedded files without copying the loader or the
reconciliation logic. The reference application composes them in
`apps/platformkit/seed.go` — a page writer, a site writer and a person writer — and runs them with
`platformkit seed --tenant <slug> --as <email> [--demo] [--dry-run]`, which
`make seed` wraps.

## Limits

A record may declare no field its writer leaves behind. `Target` is the writer's
answer about the record's fields — the state it reconciles in `Fields`, the
instruction its creation applies in `CreateOnly` — and a declared name in neither
is a declaration the run read and threw away. The service refuses it at the
field's own line, before the row, the mapping and the owner's event, the way it
refuses a command the writer does not offer. Writing the record's other fields and
reporting the run a success would be a seed that claimed a file it had partly
ignored.

A writer may carry a declared value as `Target.CreateOnly`: the fields its record's
creation applies and no later run reconciles, because the declared value is read
against the run's clock (`+3d` means three days from the run that wrote the row)
and the field itself belongs to whoever holds the record afterwards. `Decide` never
reads them, so a rerun of the same file reads as unchanged; `Create` applies them
and `Update` must not. The alternative is a value that moves with every clock and
so reports an update on every run forever, rewriting a field a person can change
through its own screen. Two of the reference application's writers use it: the
task writer for a declared `dueAt`, and the file writer for the bytes of the
record's own `asset`, which an upload writes once and no later run patches.

The demo refusal reads `tenants.demo` under the run's own transaction, not the
`tenancy.Tenant` value on the context, so a caller that builds its own tenant value
cannot bring demo records to a tenant whose row says false. A prune whose owner row
is already gone costs its mapping and the run continues: the write that finds none
is never refused, and a file with `prune: true` does not fail every later run once
somebody has deleted the row through the product.

A reference declaration belongs to a `Writer`, not to a file, so a resource's
references are read once however many kinds declare it — `pages` may name its
parent edge in `starter/pages.yaml` and `demo/pages.yaml` without the second file
making the graph a duplicate. And a key any file in the run declares is never a
prune candidate, whichever kind's mapping holds it: a record that moved from the
starter file to the demo file keeps its row, its ID and its history, and the only
thing that moves is its `seed_keys` mapping, which follows the file that declares
it now. Without that move the file which let the record go would delete a record
the other file still declares, and would do it in the same run that reported the
record `UNCHANGED`.

A record's identity is the key its owner stores, not the spelling a file wore.
`Resource.CanonicalKey` names that spelling — content's is `contentcontracts.Slugify`,
a person's is `usercontracts.CanonicalEmail` — and the run looks provenance up in
it, writes provenance in it, keeps the prune keep-set in it and resolves a
reference through it, so `About The Team` and `about-the-team` are one record with
one mapping: the second run reports it `UNCHANGED` instead of writing it again, and
a pruning file that respells a key keeps the row instead of deleting the record its
own new key had just reported unchanged. Where the field a reference stores *is*
the target's key — a site's home slug is a content slug — the referring writer
passes the reference's key through the same owner function, so what it saves is the
address the target row actually wears. Two declarations whose keys fold to one
identity refuse at the second line, because one row cannot meet two sets of fields.
`CanonicalKey` nil means the owner stores the key exactly as the file wrote it —
which is the answer for a resource with no natural key, where the mapping's own
UUID is the only identity. A tenant whose provenance was written before a writer
declared that spelling holds the file's text in `seed_keys`, which is a second name
for one row: prune therefore also refuses any candidate whose row this run matched
for a record some loaded file declares, so the older mapping is neither honoured nor
destructive. The reference application's shipped keys are already in their owners'
spellings, so no row in this checkout's data is in that shape.

What that leaves open is spelling *between* two records: a reference must name its
target the way the file that declares the target spells it, because the graph is
built from the declared text. A reference that spells an existing record its
owner's way resolves — provenance is in that spelling — while one that names a
record this same run is about to create, in a spelling the declaring file did not
use, refuses for a missing target in both `Plan` and `Apply`. That is a refusal
that writes nothing, which is the safe side; it is not a resolution, and the
sentence above is the honest shape of the rule.

A natural key is the field a writer looks a row up by, so what it can *meet* is
whatever carries that value, and a key whose text is not that field meets only
somebody else's row. The reference application's task writer names `title` while
its files' keys are seed names — `take-the-tour` declares the title
`Take the tour of the site` — so the one row a task key can meet is a person who
titled their own work like a seed key, and that row's title differs from the
file's, so the run refuses it as unowned rather than adopting, editing or
removing work it did not write
(`TestATaskTitledLikeASeedKeyRefusesTheRunAndWritesNothing`). Renaming a shipped
key to the title its record declares is that application's own tidying, not a
kernel rule; what the run does meanwhile is refuse, and a refusal writes nothing.

A command's seed run carries a person. `seedGrants` refuses one that carries
nobody, and the command resolves `--as` to a user of the target tenant inside that
tenant's own transaction before any grant is asked; the roles it checks are the
rows the tenant holds, not a credential a caller asserts. Those rows are read
again, in the run's own transaction, at the moment each grant is asked, because
what `--as` resolved is a snapshot of a few seconds earlier and a snapshot is not
an authority: a transaction that revoked the actor's roles or switched the person
off in the meantime has committed, and the run refuses rather than writing a row
whose author holds nothing. Both of the people a run
names are asked whether they can still sign in: `seedOperator` refuses an operator
whose account the installation tenant has deactivated, and `seedActor` refuses a
person named by `--as` who is not active, because a deactivated row keeps its hash
and its roles and would otherwise authorise a write through somebody the tenant
switched off. What no read closes is the revocation that commits after the last
grant is asked and before the run commits: only a lock on the person's row would,
and `seedActor` deliberately does not take one, because a seed run holds its
transaction across every record it writes and would hold the lock with it.

A tenant's own creation is the one run with nobody to ask, and it arrives through
`Service.ApplyProvisioned` rather than through a hole in `Apply`. Its proof is
state, checked in the authoritative transaction, not a token: the tenant holds no
`seed_keys` row at all, and every record the selected files declare is still
absent, so the run can only create. The reference composition adds the third
condition its own hook can check about a table the seed does not own — the tenant
holds no person — and composes that hook second in the literal `OnCreate` list,
after `auth.SeedRoles`, because the people a demo seed invites hold the roles those
lines just created. What `ApplyProvisioned` can therefore write is the records
every tenant of the application is created with, through the same owners and the
same events; anything else names a person.

The seed publishes nothing of its own. Every write goes through its owner, so the
owner publishes the event; what the seed adds is the attribution carried beside it.
`Apply` and `ApplyProvisioned` put `events.WithAttribution` around each owner write,
and the outbox row (migrations/000037) and the CloudEvents envelope the relay
publishes record `actor_kind=seed`, the record's own `source_file` and `source_line`
and the `initiator` the run named — with `actor` left NULL, because no session wrote
a seeded row and a command line is not a login. `Plan.String()` names `file:line`
for every record it decided. T-0190 translations have no owner path in this checkout, so `i18n` is
reported as skipped. A caller must not claim a successful apply before its
enclosing transaction commits.
