# Entity routes

`kit/rest` is an entity's projection onto HTTP. A module declares one `Spec[T]`
per entity in its manifest — module, entity, path, permissions and hooks — and
`Spec.Mount` registers the operations it offers on the `httpx.API`: all five, or
the ones `Operations` names, each with its declaration, emitting the events
`Spec.Events()` names and registering the `httpx.Resource` the admin generates
screens from. `Operations` is one field answering one question — which routes
does this resource actually have — and the route table, a generated page's doors,
the catalogue's `operations` key and what a boot gate is told about are all read
off that answer, so a New button over an address that mounts no POST is not
composable. A verb left out mounts nothing: the router answers 405 where the
address serves another verb and 404 where it serves none, never a refusal naming
a permission nobody asked for. A set with no list has no workspace address, so
such a resource mounts no generated page and publishes no screen; it writes its
own pages, the way `modules/admin` writes the ones a Spec cannot describe.

`ReadAuth` and `WriteAuth` are the same honesty about the guard: a route answered
by something that is not a permission — `httpx.SignedIn()`, an operator's own
tenant — is declared there rather than in the `Read`/`Write` shorthand, which
refuses to mount beside it. The plan feature such a declaration may name with
`.Needing` is asked wherever the declaration is read — the route, the six
closures, a page's two doors and the catalogue's entry, all through one
`mayDeclare` — so an entry no caller's plan reaches is in no document, and the
`402` a route answers is the answer the in-process doors give as well. A
`WriteAuth` that asks no grant may not carry `Update` or `Delete`: the generic
routes check whose tenant a row is in and never whose row it is, so a write about
one row the caller names is a `rest.Command`.
`Singleton[T]` is the same for a tenant's one row: a read and a PUT, no list
and no id in the path. Start with [Spec](rest.go), [Mount](mount.go) and
[Command](command.go);
[modules/task](../../modules/task/README.md) is the reference Spec.

Beside the five routes, `Command` mounts a verb on a row or the collection
with its own body and authorization, and `Operation` is a typed projection
with a custom path sharing the transaction and error handling. Updates and
deletes lock the live row first — an update before merging, validating and
snapshotting, a delete before removing the row — and both then ask that row
whose tenant it is, because a lock proves a row is there and not that it is the
caller's: a policy that lets every tenant read one shared catalogue lets the
read answer a row the request may not write, and a DELETE produces no new row
for a `WITH CHECK` clause to inspect, so it has only its `USING` clause to lean
on. A body that names no column stops at those two checks, so it neither
writes, validates nor publishes.
`Fault` maps kit/crud errors to problem documents; `FieldErrors`, `Values`,
`UpdateValues` and `Writable` type a submitted form by the schema; `Display`,
`Text`, `Humanize`, `FieldLabel` and `FieldHelp` delegate to
[kit/entity/display](../entity/display/display.go), the one way a value is
shown, which the generated screens read without linking this package.

## Composition (typed PATCH)

Resource and command `int64` fields use the [HTTP integer contract](../httpx/README.md#exact-signed-integers).
Numeric output is deprecated; request the explicit decimal representation to
preserve every digit in JavaScript clients. Domain fields and database columns
remain signed integers. PATCH, command defaults and screen row/form handoffs
bind through the same exact decoder; native `int`/`int32` values stay numeric.
Resource row maps retain untyped numbers as `json.Number`, so a form does not
lose digits before binding them to a declared field.

**Reused** — `Spec`, `Command`, `merge`, `row` and `decode` keep their field,
authorization, transaction and outbox decisions.
**Added** — exact integer binding at their existing JSON seams, because an
untyped float map loses digits before a writable field can validate them.
**Made reusable** — the same full-range CRUD sequence runs against PostgreSQL
and `porttest.Store`, including silent refusals and competing revision checks.

**Reused** — `Spec.Schema`, `crud.FieldNamed`, `foldedName` and Huma's entity
schemas provide the writable names, types, formats, enums and media annotations.
**Added** — `httpx.SetPatchSchema` replaces the untyped map with `<Entity>Patch`:
closed, optional properties, excluding Base, `Immutable` fields and defaults.
Nullable pointers/lists admit null; the body stays required. Attachment after
registration preserves decoding, merge refusals, validation and tenant checks.
**Made reusable** — each API owns its components for Update-enabled Specs;
conflicting shapes under one name refuse mounting.

`Immutable` names the fields a command owns. Every door that reads a body — the
JSON create and patch, the two a page calls beneath HTTP, and `Values` beneath a
create form — asks its keys the decoder's own question: one folding onto a
declared field, `author`, `Author`, `AUTHOR`, answers 422 naming the field as
declared, and none of that body is stored. `UpdateValues` drops them instead: an
edit form renders them read-only and a browser posts a read-only control back.
`Singleton` declares none to refuse. A write that named no column changed
nothing, so it says nothing: no `UPDATE`, no validation, no event, `updated_at`
where it stood. That silence is about writing and never about ownership: whose
row it is gets settled first, the same way a body that names a column settles
it, so a row the request's tenant may not write answers 404 whatever the body
omits — on a table whose read policy shows one shared list to every tenant,
row-level security lets the row be read, and it is the write that refuses.

`Gate` is the door in front of a write, asked after those checks, after the row is
locked and the body merged, and before anything is written: this row, these fields,
now, in the caller's own transaction. It exists because the generic PATCH is mounted
from this file — a rule that stood in a module's own `Save` covered one door per
module and left the other three open, which is what `modules/site` had to do on its
singleton. The fields it is asked about are computed from the merged entity's
canonical JSON and never from the request's key list, which is what makes a body
that resends the current value not a change and a generated form that redraws the
whole record writable. A command carries no field list: the kernel cannot know what a
command writes without running it, and running it is the write, so the question names
the command's verb and the answer — `modules/change`'s `contracts.Protection` in the
reference application — says what that verb moves. A gate that refuses writes
nothing, publishes nothing and runs no hook, and it is not an authorization decision:
the grants answer whether this caller may write at all, the gate answers whether the
write needs a second pair of eyes first, whichever caller it is. `Mount` refuses a
`Gate` on an entity with no `revision` field, because a write refused on the grounds
that the row may have moved has to be able to say what it moved to; the kernel moves
that number in the same `UPDATE` as the data, which is why no entity behind a gate
moves it by hand.

The `revision` number is the server's own, and the schema says so. `entity.ServerOwned`
is the one question — a field Base contributes, or the row's write counter — and every
door that takes a value from a caller asks it: `merge` refuses a PATCH that names the
field, `createRow` discards what arrived for it exactly as `crud.Reset` discards an id,
the generated form draws no control for it and its reader refuses to post one back, and
the `Changed` computation never asks about it. The reason is one sentence: a number a
caller can type is not a number a stale write can be refused with, and the whole of the
stale-base rule above rests on it. A client that wants to *write conditionally* quotes
the number instead of setting it — the item read and the patch answer `ETag` for a Spec
whose entity carries the counter, `If-Match` on `PATCH` and `DELETE` is checked against
the revision the lock found, and a tag the row has passed is 412 naming the number it is
on now. A malformed tag (`*`, a weak tag, a bare number) is 422 rather than ignored, and
an absent tag is allowed, which is what keeps every client written before the door — the
documents this repository checks in, the mobile flows — working. `If-Match` is asked after
`RecheckTenant`, so a foreign row answers 404 and never gives away its number; on a
resource whose entity carries no write count there is no tag to quote, and the request is
refused rather than silently unwritten.

`Command` also checks `entity.ServerOwned` over its derived argument fields before
registering any command or operation. An integer JSON field named `revision`, or
a read-only field contributed by an embedded `Base`, panics with the resource,
verb and first offending JSON field named. Hidden, system, collection and operator
commands follow the same rule. Use `IfRevision int64` tagged `json:"ifRevision"`
for an observed revision, then map it to the domain condition inside the callback.
The callback still owns authorization, tenant scope and revision checks in its
transaction. This declaration check runs during route composition in `App.Build`,
before deployment effects; `Validate`, `Explain` and `Describe` do not run routes.
It covers the fields the schema derives, without inspecting nested payloads or
map keys. Accepted commands retain their existing JSON decoding and error mapping.

That compare is `crud.RecheckTenant`, and it lives in
[kit/crud](../crud/crud.go) because the rule already lived there, inside
`Update`. A second compare in `kit/rest` — that package reading the row's
`TenantID` against the transaction's own — would be a second opinion about
tenancy free to disagree with the first, so one function serves `Update` and
both doors. That is an addition to `kit/crud`'s exported surface, and a
deviation from this change's own brief, which asked for no new exported API: the
smaller of two costs, since the other was a compare that could disagree. Asked
about nothing at all it answers `ErrInvalid`, the answer `Create` and `Update`
give for nothing to act on, and never a panic — a door whose precondition is a
sentence is a door that crashes the request that asks it.

Prerequisites: an entity embedding `crud.Base` with a `TableName`, its
migration, and the permissions the manifest declares. Tests need the
development database: `make up`, then `make test TEST_PACKAGES=./kit/rest`.

## Composition (command argument ownership)

**Reused** — `Command`, `commandFault`, `crud.FieldsOf` and `entity.ServerOwned`
provide the declaration boundary, field metadata and ownership rule.
**Added** — the ownership check and corrective diagnostic at command declaration,
because runtime merge refusal cannot prevent an unusable command from composing.
**Made reusable** — declaration, JSON and application-build conformance cases
exercise the same guard for every module's command inputs.

## Composition (one guard, one command declaration)

A guard has two spellings — `Read`/`Write` with `OperatorRead`/`OperatorWrite`, or
`ReadAuth`/`WriteAuth` as an `httpx.Auth` — and they answer one declaration. Neither
is merged with the other: stating a guard in both is refused, and stating it in
either lands the route on the surface the answer names and reaches `kit/app`'s
manifest cross-check with its operator fact intact. A command is now declared the
same way once, as `CommandSpec{Verb, Summary, Doc, Emits, Auth, Collection,
Options}` handed to `Mount` or `MountResult`; `Command` and `CommandResult` take the
same command as positional parameters, delegate, and are deprecated. Migrating one
is the spelling: a guard stated in `Options`, where `Command` kept it, still mounts,
and one the two fields answer differently is refused, as is a collection route
`Options` asks for and the `CommandSpec` does not — a bool has no spelling for
"nothing said", so that pair is the one doubling a reader cannot resolve alone.

**Reused** — `httpx.Resource.ReadAuth` and `WriteAuth`, which already owned the
derivation, and `(*httpx.API).guard`, which already computed both sides once per
registration; `registerCommand`, which stays the one body of both command forms;
`ui/resource.Resource.offers`, already how the New button is gated. **Added** —
`CommandSpec`, `Mount` and `MountResult`, because nine positional parameters, three
of them adjacent strings, cannot be read at the call site, and nothing here or on
the shelf declared a command as a value; and the write-back of the derived guard
into `ReadBy`/`WriteBy`, which every reader of a registered resource consults.
**Made reusable** — a list with no record route is now a legal resource, and
`ui/resource` asks whether a record page exists before it links a row to one, the
question the page side and the door side already asked. No module's own list page
changes shape, because every module resource that offers `list` offers `read`; the
reference application's picture fixture is the list whose rows do change. And the
refusals left at mount are about a fact said twice, not about which field said it,
so the next guard that is not a plain permission needs no new field.

## Composition (T-0360)

**Reused** — the richtext pass that already parsed every field of a save before any of it
was written, the one transaction that save runs in, and `FileUses` as it stood: a port
that files one field's uses for one record. **Added** — `Spec.Audience`, a record's answer
to "is this body served to somebody who has not signed in", asked once per write from the
row the write already holds and never from the body being saved; `FileUsesOrder` with
`SaveUses`, the optional half of that port that is told the whole save — every richtext
field of it, or nothing at all for a record being deleted — before its first field is
filed; and `recordLocale`, which is one answer at the save door and at the delete. **Made
reusable** — the save-wide lock order is kit/rest's to offer and the ledger's to decide:
a resource with two richtext fields gets it by wiring a recorder that answers, which is
what two records sharing two pictures in each other's field order would otherwise lose to
Postgres (SQLSTATE 40P01), and a case in this package's own tests holds the order the kit
owes that recorder.

## Composition (T-0185)

**Reused** — `kit/httpx`'s closed guard set (`Public`, `SignedIn`, `Permission`,
`OperatorPermission`, and the fifth, `AnyCredential`, which arrived on main while this
branch was open and is admitted by the same `ReadAuth`/`WriteAuth` field, refused by the
same rule that refuses a guard naming no grant on a generic write) and the `mayUse` that
already answered all three questions for a command; the five verb strings the operation ids (`<module>-<entity>-<verb>`) and this
package's own `writes` map are already built from; `Singleton`'s precedent of refusing
rather than handing back a closure whose route does not exist; and `ui/screens`'s
`derived` rule, which prints a catalogue key only once the derivation has stopped being
true. **Added** — `Spec.Operations`, `Spec.ReadAuth` and `Spec.WriteAuth`, `httpx.CRUD`
with `CRUDValues`, `httpx.Resource.Offers`/`OperationWords` and `screens.Entry.Operations`:
nothing existing said which of the five routes a resource has — `Singleton.Write == ""`
is a singleton's shape and its own doc says a Spec is not one, `CommandOptions.Auth` is
one command's guard, and a shell could not learn that the address behind its New button
mounts no POST. **Made reusable** — the operation set travels as one value from the Spec
to the router, the registered resource, the generated pages, the catalogue entry and the
OpenAPI document, so the route table, the doors and the wire cannot disagree about one
resource; `Offers` and `OperationWords` are the two accessors any later shell reads; and
the ratchet written beside the versioned catalogue
(`apps/platformkit/catalog_version_test.go`) is the pattern for every additive key that
catalogue gains next.

## Composition (T-0138)

**Reused** — `crud.GetForUpdate` for the lock the gate is asked beneath, `crud.RecheckTenant`
and `crud.Update` unchanged (the gate hands them the same entity and the same column list),
`Spec.Immutable`'s mount-time name check as the model for refusing an incoherent door, and
the error mapping every other write here already uses: a gate that returns `crud.ErrConflict`
gets 409 and a gate that returns a problem document gets that document, so the door needed no
new status. **Added** — `Spec.Gate`, `rest.Gate`, `rest.Write`, the four verb constants,
`RevisionField` and the one `Changed` computation over the merged entity's canonical JSON:
nothing in `kit/rest` or `kit/crud` had a pre-write hook (only `AfterCreate`/`AfterDelete`,
which run after the write) and no way to ask "which fields does this write move" — which is
the question `modules/change` asks and the one a request body cannot answer honestly. **Made
reusable** — the same `Check` answers the JSON PATCH, the DELETE, a generated form's POST, an
in-process `Spec.Update` beneath a worker and a lifecycle command, because they share
`updateRow`/`deleteRow` and this stands in those rather than in a router; the kernel's own
`revision` bump rides the one `UPDATE`, so an entity that grows the field gets a number a
diff can quote without writing a trigger, a column of its own or a second writer; and a
command is asked about by verb, which is what leaves a command nobody annotated still
answered by the module that owns the row.

## Translating a record

A field tagged `i18n:"translatable"` gives a resource the same text in another
language. Two interfaces carry that, and the direction each points in is the reason
neither lives in a module.

`Translations` is the door a `rest.Spec` knocks on to read and write one record's
translations. The dependency runs module→kernel, so the shape the kernel asks for has
to be the kernel's own, and `modules/translation` implements it. The Spec holds it as
one field wired by hand at composition, the same way `RichTextFiles` is wired; nothing
discovers it.

`TranslationSource` points the other way, because one question is unanswerable from
the translations table alone: "missing" means *no row*, and which records exist is a
fact about the entity's own table, which another module owns. So the row set arrives
at the translation module through a port the translation module does not own and
cannot fill — house rule 5, a higher-tier collaborator reached through a port declared
here and implemented at composition — and the only reflection anywhere is the kernel's
own, one line per mounting module:

	Sources: []rest.TranslationSource{rest.TranslationSourceOf(contentSpec)}

There is no registry and no discovery: the composition writes the list and the
compiler checks it.

### Reading a record in another language

The source language is the tenant's default, because the record's own write door
insists on it — a translation of the tenant's own language is refused as a mistake, so
the row can only ever be in the default. No caller passes a source language and none
can disagree.

Completeness is per field and never per locale. One field is answered in the asked
language when that field is reviewed against a source that still matches; a field whose
only text is an unreviewed draft is not an answer, and neither is one whose source has
moved since — that text says something its author no longer says, and it is the kind a
search engine would index under a language flag and keep for a year. Both are shown to
the workspace, which sees the label, and neither to a reader, who sees that field in
the source and nothing else. A reader who asked for Portuguese of a page whose title is
translated and whose paragraph is not is given the Portuguese title: withholding the
part the installation has because one field is missing answers a reader with less than
there is.

A locale counts as one this record *may be read in* — its hreflang alternate — only
when every translatable field is reviewed in it. That strict question is a different
question from what to print. The other locales are asked about whatever the reader
named, including nothing: the page at the bare address is read by a crawler that
decides what to index from the alternates in its head, and a translation made after the
page was written is discoverable only if the source page lists it.

### Writing a translation

Four doors, all of them commands of the record rather than routes of the translation
module, and that placement is the design:

- **The guard is the record's own write permission.** A stranger who cannot edit the
  page cannot write its Portuguese either. There is no second permission to grant, to
  drift out of step with the first, and to audit against separately.
- **The source row is locked before its text is lifted**, by `crud.GetForUpdate`, the
  same lock the record's own PATCH takes before it merges. A translation measured
  against a source the caller never held is wrong the moment two people write at once.
- **The value is validated by the field's own rules** — the same `richtext.Prepare`,
  the same ceiling and the same `RichTextFiles` port the record's own write runs —
  because the rules belong to the field and the translation module has no business
  knowing what a page body is. A translated paragraph that would be refused in English
  is refused in Portuguese; a door that skipped this would let through the back of the
  feature what the front of it never allows.

Two verbs name the noun they act on (`review-translation`, `suggest-translation`)
rather than the bare `review` and `suggest`: the verb is the command's whole address
and its operation id, and a resource that already reviews its own rows would otherwise
find two doors mounted at one path.

## Composition of resource steps

**Reused** — `Spec`, `Command`, `Operation`, the locked CRUD write core and existing
form/schema helpers remain the public resource path. **Added** — private route,
validation, mutation, command and form steps separate responsibilities that were
nested in the same functions. **Made reusable** — named steps shared by JSON
routes and in-process resources, with no additional write implementation.

[Declarations](rest.go), [mounting](mount.go), [declaration checks](spec_check.go),
[commands](command.go), [request binding](request.go) and [patching](patch.go)
keep their existing public signatures. [Writes](write.go) retain lock, validation,
tenant/revision/gate, file, persistence, translation, history and event order;
an empty patch only rechecks tenant ownership. [Resources](screens.go) use those
same writes, and [forms](form.go) retain absent-versus-blank and typed-list rules.
[Entry hints](entry_hints.go) keep the existing first-refusal order.
