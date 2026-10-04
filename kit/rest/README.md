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
and no id in the path. Read [rest.go](rest.go) first;
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
