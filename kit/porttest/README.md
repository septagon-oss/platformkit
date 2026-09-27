# porttest

The conformance harness a port's suite is written in. A module describes its
port — in Go, beside the port, as closures — and this package generates the
cases every port owes its consumers: the success, the retry, and the four
refusals a mutating operation owes. A case the description cannot express stays
hand-written in `Suite.Own`, with the reason it is there.

`Run(t, suite)` runs them. `Names(suite)` lists what it would run without
running it, so a module can pin its case names: in a client repository a case
name is what a requirements index points at, and a change to one should be a
diff somebody reads.

## Every field, and the case that reads it

A field no case reads does not belong in the description. That is the table:

| field | read by |
|---|---|
| `Suite.Port` | every failure sentence |
| `Suite.World` | every case; one world per case, closed on the way out |
| `Suite.Events` | the success case, the retry, and every refusal ("a refusal is not news") |
| `Suite.Classify` | every refusal whose `Class` is not `Unclassified` |
| `Suite.Ops` | the generated cases |
| `Suite.Own` | run after them, under the module's own name, unnested |
| `Suite.Assert` | a hand-written case, which takes the floor from the suite it belongs to |
| `Case.Name` | the subtest name, verbatim |
| `Case.Because` | the floor: a hand-written case with no reason is refused |
| `Op.Name` | the first half of a generated case name, and uniqueness |
| `Op.Mutates` | whether the retry and the four refusals are owed |
| `Op.Ready` | every case of that operation |
| `Op.Call` | the success case, and the retry, which compares what it answered either side of the second call — and refuses a mutating operation whose rendering is empty, since two empty strings compare equal |
| `Op.Snapshot` | the retry and every refusal: "writes nothing" |
| `Op.Publishes` | the success case, in order |
| `Op.Refusals` | one case each |
| `Op.Names` | the generated case name where the module has its own sentence |
| `Op.Skip` | the floor, which asks for a case or a reason |
| `Refusal.Kind` | which floor case this answers, and the order they run in |
| `Refusal.Name` | the case name, verbatim |
| `Refusal.Provoke` | the state the refusal answers |
| `Refusal.Call` | the refused call |
| `Refusal.Is` | whether the error is that refusal — the port's own answer |
| `Refusal.Class` | asserted through `Suite.Classify` |

## The cases, in the order they run

Per operation:

| # | name | generated for | asserts |
|---|---|---|---|
| 1 | `<Op>: the operation says what it did` | every operation | the call succeeds and publishes exactly `Publishes`, in order |
| 2 | `<Op>: the same command twice writes nothing and says nothing` | `Mutates` | the second call succeeds, the snapshot does not move, nothing is published, and it answers what the first call answered |
| 3 | `<Op>: an unknown row is not found` | `Mutates` | the refusal is the one named, in the class named; nothing written, nothing said |
| 4 | `<Op>: a caller with no grant is refused and writes nothing` | `Mutates` | as above |
| 5 | `<Op>: a revision the row is not at is refused and writes nothing` | `Mutates` | as above |
| 6 | `<Op>: another tenant cannot reach the row` | `Mutates` | as above, and the row is still there for the tenant that owns it |
| 7 | the module's own sentence | each `Named` refusal | as above |

Then `Suite.Own`, in the order the module wrote them.

A sentence the module gives — `Op.Names[Kind]` or `Refusal.Name` — is used
verbatim and replaces the whole generated name, prefix included. That is what
keeps an evidence link pointing at something when a hand-written case becomes a
described one.

## What the floor refuses

`Run` fails the description before it runs a case when:

- a mutating operation carries neither a refusal of a required `Kind` nor
  `Skip[Kind]` with a sentence in it;
- a `Skip` has no reason, or names a case that is described, or is filed under
  `Named`, or excuses the success;
- two operations share a name, two refusals share a name, or two cases would run
  under one name;
- a hand-written case has no `Because`, no name or no body;
- a mutating operation has no `Snapshot` — "a refused mutation writes nothing" is
  unassertable without one;
- a refusal has no `Is`: what counts as that refusal is the port's own answer,
  and a harness that guessed would relax an assertion;
- the description would run no case at all: a suite that asserts nothing passes
  every implementation of the port, including the one that does nothing, and
  deleting the last operation from a converted suite looks exactly like that.

Two mistakes a world makes are caught in the case rather than before it, because
that is the first moment anybody can know: a world that hands back a zero
fixture, and a world that returns without ever running the case it was handed —
which would otherwise report every case green having touched no implementation
at all.

## What a description cannot express

These stay hand-written, in `Own`, with the reason beside the case. They are
measured categories rather than an escape hatch:

1. the words of a refusal, where the sentence itself is the requirement;
2. the contents of a success — what the page carries is the port's domain;
3. a differently composed collaborator: a world with no register, or an
   unreachable one, builds its own world;
4. separation of duties — two people who both hold the grant is not a grant
   question;
5. the order, paging and filtering of a read;
6. a sequence of more than one command with an assertion in the middle;
7. a loop over spellings or inputs inside one case.

And one thing it refuses to express: a sequencing language. A description rich
enough to generate a publication — gather the service, its figures, their
reviews, ask the register, then write the supersession and two events — would be
a second language, less legible than the decision function a reader can read
today.

## The fake's plumbing

A module's fake embeds `*Fake` (a `Clock`, a `Grants` table and a `Recorder`)
and keeps its own stores and its own decisions:

```go
type Fake struct {
	*porttest.Fake
	notes *porttest.Store[contracts.Note]
}
```

- `Store[T]` is partitioned by the tenant on the context and panics where the
  context names none: there is no door here that takes a tenant as an argument.
  It hands rows out by value and lists them in insertion order, so a list case
  cannot pass by luck, and another tenant's row is `crud.ErrNotFound` — the same
  answer as a row that never existed.
- `Seed` puts a row the way a create route would: an id, the clock's stamp and
  the entity's own `Validate`, which it panics on. It does not stamp a revision:
  the kernel's `Base` carries none, so which field holds one is the module's
  answer and `Command.Revision` is where it gives it.
- `Recorder.Emit` refuses an event name the module did not declare.
- `Grants.Holds` answers the recheck inside the command. The module wraps its own
  denial sentinel around a false answer, because whose sentinel that is belongs
  to the module.
- `Do` runs a single-row command in one order — grant, load, revision, decide,
  and only then apply and emit — so "a refused mutation writes nothing and emits
  nothing" is structural rather than a discipline each fake keeps. It checks the
  event names before it writes, so state and what the command says commit
  together or neither does. A command that gathers more than one row calls the
  parts in that same order and is held to it by the generated cases.

What a fake still cannot claim, and what each module repeats in its own words:
row-level security, the unique indexes, the append-only triggers and the row lock
that settles two writers. Those are database facts, tested against the schema.

## Reading the harness's own suite

`porttest_test.go` runs the generated cases against deliberately broken copies of
the fake in `notes_test.go`: one that writes before it decides, one that says
something on an idempotent retry, one that stores the right row and answers a
revision it is not at, one with a single set of rows for every tenant. Each
proves that the case bites. `Run` reports through a narrow internal
reporter so that a test can watch a generated case fail without failing the test
that is watching it.

## Reused, added, made reusable

**Reused**: `kit/crud`'s sentinels (`ErrNotFound`, `ErrInvalid`, `ErrConflict`)
and its `Entity`/`Validator` contracts, `kit/entity.BaseOf` for the stamp a seed
leaves, `kit/tenancy` for the tenant and the actor on the context, and
`testing`'s own subtests for every case name. The SQL harnesses this package's
consumers write keep using `kit/db/dbtest.Schema` unchanged.

**Added**: the description (`Suite`, `Op`, `Refusal`), the cases generated from
it, and the fake's plumbing — `Store`, `Seed`, `Clock`, `Grants`, `Recorder`,
`Do` — because the repository had no kernel clock, store or recorder to compose:
every fake had grown its own map and its own event slice.

**Made reusable**: the floor itself. "A refused mutation writes nothing, emits
nothing and returns no stale row" was a discipline each suite kept by hand, in
its own words, at whichever commands its author remembered; it is now
`World.Refused` and four generated cases that every port either answers or
declines in writing.
