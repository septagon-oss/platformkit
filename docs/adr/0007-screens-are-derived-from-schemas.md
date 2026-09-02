# 7. Screens are derived from schemas

Status: accepted, 2026-09-02

## Context

The previous codebase's admin surface was 52 Stimulus controllers, a block
library, a descriptor pipeline that turned component manifests into delivery
graphs, and one hand-written set of list, detail and form templates per entity.
Fifty-four modules meant fifty-four sets. A field added to an entity appeared in
the API immediately and on a screen when somebody remembered, so the two drifted
in one direction only: the screens were always behind, and the ones nobody used
were always wrong.

The generic half of that work is the same for every entity. A list is a page of
rows with a sort and a filter. A detail is the fields. A form is one control per
writable field, and the control is decided by the field's type. None of it needs
to know what a task is.

What it does need is the entity's shape, at runtime, in a process that did not
compile against it. `kit/crud` already derived one — `crud.Schema`, from the
struct tags that were already there for the API — and `kit/rest` already
attached one to every `Spec`. Nothing read it. The E2 review said so.

## Decision

**A screen is generated from a resource's schema, and a hand-written screen is
an exception that has to earn itself.**

`rest.Spec.Mount` registers an `httpx.Resource` beside its five routes: the
names, the path, the two permissions, the `Immutable` list, the schema, and the
five operations bound to the entity's type as closures. `modules/admin` reads
that register in its own `Routes` and generates seven pages per resource — list,
detail, create form, edit form, the two writes and delete.

Everything on those pages comes from the schema. A `select` exists because the
struct says `enum`. A textarea exists because the column is `type:text`. A field
is required because it says `validate:"required"`, absent from the form because
it is `readOnly`, absent from the list because it says `ui:"hide:list"`, and
shown read-only because the `Spec` named it `Immutable`.

Five screens in the whole application are written by hand, and each says why in
its own comment: sign-in, the dashboard, the health page, the tenant switcher
and the component gallery. A sixth arrives when an interaction cannot be
derived — and the test of that is whether it is about *this* entity rather than
about entities.

## Consequences

- Adding an entity to this application adds seven screens and no code. Adding a
  field to an entity adds it to every screen, in the same commit as the API.
- The screens cannot outlive the API's rules. They call the same closures the
  routes do, in the same request transaction, so a write from a form publishes
  the same events, refuses the same read-only fields and answers with the same
  three errors. There is no second implementation to keep honest.
- The screens cannot be more permissive than the API. Each declares
  `Permission(resource.Read)` or `Permission(resource.Write)` — the resource's
  own — so the boot gate sees them and the request-time middleware enforces
  them. The navigation asks the same `httpx.Authorizer` the kernel enforces
  with, so a link that is shown is a link that works.
- The shell is composed last. `kit/app` calls each module's `Routes` in
  composition order, so a module mounted after `admin` registers a resource
  whose screens were already generated — which is to say, were not.
- **The cost is that a generated screen is generic.** It cannot know that
  `assigneeId` is a person: it renders the identifier and says there is no
  picker for it. It cannot know that `Assign` is the door that field belongs
  to: it renders the field read-only and says a command owns it. Those are
  honest and they are not good, and the answer when one of them matters is a
  hand-written screen for that interaction — not a tag that makes the generator
  cleverer.
- A schema that describes nothing renders nothing. A field whose Go type is
  outside `crud.FieldType`'s closed set is in the JSON and on no screen, which
  is the same rule the sort and the filter already followed.
