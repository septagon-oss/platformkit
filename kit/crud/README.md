# crud

The five operations every tenant-owned resource needs, over the transaction the
request already holds.

## Which door to call

Two families of the same door differ in one thing: whether they ask the caller's
context.

`GetCtx`, `CountCtx`, `ListCtx`, `SoftDelete` and `DeleteRow` take the caller's
context and ask it before they send a statement. A caller that has ended is
refused with an error wrapping its own cause, not with a row that is not there.
None of them rebinds the statement — `db.Tx` already hands each operation a handle
bound to that caller, so a second context could only replace the transaction's,
and the handle these five send comes from `begin`, which never hands the caller's
context to a statement.

`Get`, `Count`, `List` and `Delete` are the same doors for a caller with nothing to
cancel, and are deprecated in favour of the `*Ctx` forms.

`Create` and `Update` ask nothing of the caller yet: their context reaches the
entity's own `Validate` and nothing else, so an ended caller still writes through
them until their own task gives them a guard. Until then a caller that must be able
to stop mid-write uses the `*Ctx` reads and checks its own context before it calls.

## Reused, Added, Made reusable

**Reused** — `kit/entity` for `Base`, the schema and the field metadata, and
`kit/fault` for the three refusal sentinels, both re-exported here as the same
values rather than copies. **Added** — this page, because the choice between the
`*Ctx` doors and the deprecated ones is a caller's contract of more lines than a
package doc that also has to say what the package is can hold. **Made reusable** —
the shape of that contract: a door family that asks the caller's own ending, and a
deprecation that names the family to move to.
