# Task module

`modules/task` is the reference module: one entity, a `rest.Spec` at
`/api/v1/task/tasks` guarded by `task:read` and `task:update`, explicit
lifecycle commands, an optional tenant-qualified policy check and the SLA
sweep job. Read [module.go](module.go) for the manifest, [contracts/](contracts/)
for the entity, service, events, permissions and the
[conformance suite](contracts/tasktest/), and [domain/](domain/README.md) for
the resolution rule that needs no runtime.

Its SQL is [migrations/](migrations/), embedded and exported as
`task.Migrations`; the manifest hands the kernel the same files, and a test
composes it with `dbtest.Schema(t, task.Migrations)`.

Compose it with `task.Deps{Service, Policy, Tenants, SweepEvery, Gate}`, or name
`task.Module` in `Use` and let the resolver fill `Policy` and `Gate` from whatever
the composition provides and put the desk back under `contracts.Service`,
`contracts.Creator`, `contracts.Reader` and `contracts.LockedReader`; both inputs
are optional, and
[modules/auth/policies](../auth/policies/README.md) shows the
Topaz adapter. The generated screens appear at `/app/task/tasks` with
no code of the module's own. `make test TEST_PACKAGES=./modules/task/...`
needs the development database; the domain package's tests need nothing.

## Authorization

### Permissions

The manifest in `modules/task/module.go` (`permissions`) declares two keys, whose constants are in `modules/task/contracts/permissions.go`.

- `task:read` guards the generated read routes of the `rest.Spec` (`spec.Read`), the generated screen `task/tasks` and the "Tasks" nav entry (`Nav` in `Module`).
- `task:update` guards the generated write routes (`spec.Write`) and the three commands `assign`, `resolve` and `check-sla`. `RegisterRoutes` in `modules/task/internal/handler.go` passes `rest.CommandOptions{}`, and `rest.CommandOptions.Auth` in `kit/rest/rest.go` says its zero value is the Spec's own write permission.

The module declares no export and no event handler (`Subscriptions: nil`). The SLA sweep (`internal.SLASweep`) is a job, not a request.

### Object scope

`Service.authorize` in `modules/task/internal/policy.go` consults `tenancy.Policy` through `tenancy.RequirePolicy`. Two commands call it, after locking the task row with `crud.GetForUpdate`.

- `Assign` (`modules/task/internal/service.go`) asks action `task:assign`.
- `Resolve` asks action `task:resolve`.

The request carries `Resource.Kind` `"task"`, the task id and tenant id, and the actor as a user taken from `tenancy.PrincipalFrom`. The trusted attributes are `status`, `priority` and `assignee_id` (empty string if unassigned). `Assign` also sends `requested_assignee_id`. If there is no principal the call returns `tenancy.ErrPolicyDenied`. `CheckSLA` and the generic CRUD routes do not consult the policy.

The policy is optional. With `Service.Policy == nil` the check returns nil and the route grants alone decide. A composition enables it with `Deps.Policy` or `NewServiceWithPolicy` in `modules/task/module.go`. The [substitution matrix](../../ARCHITECTURE.md#policy-substitution-boundary) defines adapter, retry and transaction limits.

The reference application (`apps/platformkit`) enables it with a rule written in Rego, `apps/platformkit/policy/task.rego`, evaluated in process by `kit/tenancy/providers/opa`: assignment is allowed, an unassigned task may be resolved by any holder of `task:update`, and an assigned one only by its assignee. `task:resolve` is the policy action, not a route grant. A refusal answers 403 `POLICY_DENIED` and is recorded as `security.denied` with the action, the rule's reason and the policy's revision (`sha256:` and twelve hex characters of the source), because `kit/httpx` hands every `tenancy.RequirePolicy` refusal of an authenticated request to the same audit hook as a missing grant.

### Duties the module enforces itself

The policy never replaces the lifecycle's state checks (see the `Service` comment in `modules/task/internal/service.go`).

- `Assign` refuses an empty assignee and a `resolved` or `closed` task (`crud.ErrInvalid`, `crud.ErrConflict`).
- `Resolve` uses `domain.Resolve`. A different resolution on an already resolved task is a conflict.
- `spec.Immutable` in `modules/task/module.go` refuses `assigneeId`, `slaBreached`, `resolvedAt` and `resolution` on the generic PATCH.

The code has no rule tying an assignee or resolver to the task's creator. That kind of rule could only come from an external policy.

### The write counter, and the door in front of a protected field

Every write of a task row moves `Task.Revision` (the column
`migrations/000050_task_revision.up.sql` adds, the same shape `modules/site` grew for the same
reason). The three commands move it in the `UPDATE` each already makes, `Writer.Save` moves it,
and a `rest.Spec` whose entity carries `revision` moves it inside `kit/rest`'s own `UPDATE` —
so no writer moves it twice and none of them moves it by hand. The number exists so a diff can
quote one: it is what a stale proposal is refused with, and what the refusal a protected write
gets quotes back.

`Deps.Gate` is a `rest.Gate`: the door [kit/rest](../../kit/rest/README.md) asks at the
lock-merge-write seam of the generic PATCH, the DELETE and the three commands. The module
answers nothing about *which* fields are protected, and imports no peer module to ask: it
publishes the vocabulary, and the composition answers, with the installation's own switch.
Under `Use(task.Module)` that answer arrives as one `Optional` edge of the same
`rest.Gate` type — the reference application provides it from its own `product` module —
which is why the door is handed to the module rather than written around the routes a
`Spec` mounts itself.

- `contracts.ProtectableFields` names the fields a proposal may move; `contracts.CommandOwnedFields`
  names the ones a command owns, and is the one list `spec.Immutable`, `Writer.Save`'s refusal and
  the composition's gate answer all read.
- `contracts.LockedReader.TaskForUpdate` is the locked read a diff is made against.
  `contracts.Writer.Save` is the one whole-row write, and the composition hands it to the apply
  path and to nobody else: it runs `Validate`, refuses a command-owned field and `status`, bumps
  the revision and publishes `task.task.updated` in the same transaction. It stands outside the
  gate on purpose — a gate in front of the door an approved proposal comes through is a wall.

**Reused** (decision 0022) — `sitecontracts.Revision`, `sitecontracts.LockedReader` and
`modules/site/migrations/000039_site_revision.up.sql` as the precedent for adding a counter to a
row that has one tenant's history on it, `kit/crud.GetForUpdate` and `kit/crud.Update` unchanged,
and `kit/rest`'s `Spec.Gate`. **Added** — `Revision`, the migration, `LockedReader`, `Writer`, the
two field lists and `Deps.Gate`. **Made reusable** — the two field lists: they are what lets a
composition stand this module's rows behind another module's door while neither module names the
other, which is the shape decision 0038 asks for.

### Public faces

None. `git grep` finds no `httpx.Public()` in `modules/task`, and every route is guarded by `task:read` or `task:update`. The module uses no `kit/limit`.

### The operator boundary

None. No permission in `permissions` sets `Operator: true`, and `spec` sets no `OperatorRead` or `OperatorWrite`. The routes are scoped to the caller's own tenant transaction.

### Provisioning

The module does not create roles. A composition grants `task:read` and `task:update` through the tenant's roles, using the auth module's roles API (see `modules/auth/README.md`). The administrator role holds the wildcard `*`, which satisfies these two keys (`docs/adr/0006-system-access-is-a-token.md`). The code does not name any other persona. To use resource decisions, pass a `tenancy.Policy` in `task.Deps.Policy`. `modules/auth/policies` has a Topaz adapter.

## Composition

**Reused** — `rest.Spec.Topics`, `events.Topic`, the transactional outbox and task's existing lifecycle contracts.
**Added** — typed CRUD and lifecycle topic declarations, because the former string publisher could pair any name with any payload at compile time.
**Made reusable** — `Spec.Topics()` and `events.PublishTo` give the next module one generated CRUD declaration and a compiler-checked publish path.

`task.Module` provides four contracts and one value: `contracts.Service`,
`contracts.Creator`, `contracts.Reader` and `contracts.LockedReader` are one
`internal.Service` put under four keys, so a module that needs part of the desk
asks the plan for that part and is answered with the desk its own routes write
through. With or without a `tenancy.Policy` in the composition, the same object
answers all four. `contracts.Writer` is deliberately not among them: `Save` is the
one door that moves a protected field with no gate in front of it, and handing it
to every composition that asked would widen that door by composition rather than
by decision — a product that composes the change-control subject builds it by name
with `task.NewWriter`.

**Reused** — `pkit.Provides` and `pkit.Put`, the one mechanism a capability is
published by (the shape `modules/site` uses to hand out its own `LockedReader`),
and the three contracts exactly as `contracts/task.go` already declares them, with
no signature moved. **Added** — three declarations and three puts, because the
implementation existed and the plan could not name it; and `internal.NewTask`, the
exported name of the creation decision the fake in `contracts/tasktest` now calls,
so the fake and the SQL service cannot answer one input differently. **Made
reusable** — the four provides themselves, which is what lets another module ask
Task to raise a task without reaching for a constructor; `internal.NewTask` as the
single entry point a double may share; and the two cases beside it — one
comparing what a consumer read from the plan with what the plan answered, one
running a fake's decision against the service's — as the shape a module with a
test double copies.

### Proving a policy substitution at this module's boundary

**Reused** — `tasktest.Fixture`, `porttest`, `dbtest.Schema`, `domain.Resolve`, the OPA/Topaz adapters and the reference product's typed declarations.
**Added** — one shared adversarial policy consumer suite, because adapter-only cases could not prove task retries, row locks, rollback or HTTP refusal behavior; task commands now return no row if outbox insertion fails.
**Made reusable** — `tasktest.RunPolicy` and the disposable `topaztest.Client` protocol fixture. The [substitution boundary](../../ARCHITECTURE.md#policy-substitution-boundary) names service fidelity and unsupported cached-reply authority guarantees.

### The reading vocabulary
**Reused** — the reading vocabulary `kit/entity` froze (`EntryHints`,
`FieldHints`, `CommandHints`, `Icons`, `Tones`, `DoneValues`) declared at the three
sites `kit/rest` already exposes (`Spec.Present`, the
`ui:`/`enumLabels`/`enumTones`/`doneValues` tags in `derive`'s grammar,
`rest.CommandOptions.Present`), the copy machinery
`kit/locale/providers/xtext` and `ui/page`'s `TenantPreferences`/`SelectLocale`
for the negotiation, and `ui/resource`'s own `readable` filter for what a form
offers. **Added** — the words themselves (this module's `Present:` literal, its
field tags and its two command labels, with their Portuguese beside them in
`messages/pt-PT.json`), because nothing existing could carry a fact only this
module's author knows; and one resolution point, the derived-key resolver
`ui/resource/hints.go`, because every other way words reached a catalogue was a
key spelled at a call site beside a string already written. **Made reusable** —
that resolver, the `screens.Text` seam and `screens.Localise`, the
`display.EnumWord`/`DisplayWord` readers, a form that honours `visibility:hidden`,
and the `hints.<module>/<entity>.<aspect>` grammar itself: any module that
declares a reading word gets it served in the request's language with no new
plumbing, and `apps/platformkit/reference_reading_test.go` is the pattern its
next delivery copies.

## Reading

The words this module says about itself, where it says them, the key a
translation is written under, and who reads them. The key is derived from the
declaration's own address — `hints.<module>/<entity>.<aspect>`
([`kit/entity/README.md`](../../kit/entity/README.md)) — so it appears here and
in the module's `messages/pt-PT.json`, and nowhere in Go.

| The words | Declared at | Copy key | Read by |
|---|---|---|---|
| `task`, `tasks` | `Spec.Present` in `module.go` | `hints.task/task.singular`, `hints.task/task.plural` | `GET /api/v1/app/resources`, which is where a shell that is not a browser learns them |
| `Work` | the same literal's `Group` | `hints.task/task.group.work` | the nav entry's group heading, on the document and on the page |
| `Due`, `Response due`, `Response overdue`, `Resolved` | the `ui:"label:…"` tag on each field in `contracts/task.go` | `hints.task/task.field.<name>.label` | the column header, the control's label and the record's term — one string, three places |
| `Assignee` | the same tag on `assigneeId` | `hints.task/task.field.assigneeId.label` | as above, plus the reference target the picker names |
| `Assign`, `Resolve` | `rest.CommandOptions.Present` in `module.go` | `hints.task/task.command.<verb>.label` | the button on the command's form |
| the `Assignee` the assign form asks for | that command's argument field | `hints.task/task.command.assign.field.assigneeId.label` | that one control's label — a column and an argument are two strings a translator may want to choose differently, which is what the `command.<verb>.` prefix is for |

Two absences belong in the table, because an absent declaration is what decides
what a screen falls back to. `status` and `priority` declare `enumTones` and no
`enumLabels`, so their words are `display.EnumWord`'s humanizing of `in_progress`
and no key exists to translate until the module writes the map. And `Task`
declares no `help:` anywhere: the line under one of its controls is that field's
`doc:` sentence, which is a developer's sentence rather than a hint string and so
carries no key — the difference between the two only shows in a language other
than English, which is why `apps/platformkit/help_language_test.go` asks it of
`content`, which declares one.

## Creation

**Reused** — `crud.Create`, `events.Publish`, `Task.Validate`, `Service.Assign` and `Service.CheckSLA`, composed by `internal.Service.Create`, and `internal.NewTask` — the same input check and normalisation, named once — by `tasktest.Fake.Create`. **Added** — `contracts.Creator`, `CreateTask` and `contracts.Reader`, plus `rest.Spec.Create`, because the generic create had no seam through which an owner could take over identity, defaults and its events. The JSON create and the web form reach the creator and publish nothing of their own; `task:create` is not a policy action, so the route's `task:update` grant stands. **Made reusable** — `rest.Spec.Create` for any entity that owns its creation, `internal.NewTask` as the creation decision a test double may share rather than copy, and `tasktest.RunCreator` with its real-database harness (savepoint rollback, outbox-insert failure injection) as the pattern for the next creator. The two rollback cases need a transaction the fake cannot be handed, so they skip against it with that reason and run against the real store; the rest of the suite, including the tenant partition and every refusal, runs against both.
