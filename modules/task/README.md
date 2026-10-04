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

Compose it with `task.Deps{Service, Policy, Tenants, SweepEvery, Gate}`; `Policy` and `Gate` are
optional and [modules/auth/policies](../auth/policies/README.md) shows the
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

The policy is optional. With `Service.Policy == nil` the check returns nil and the route grants alone decide. A composition enables it with `Deps.Policy` or `NewServiceWithPolicy` in `modules/task/module.go`.

The reference application (`apps/platformkit`) enables it with a rule written in Rego, `apps/platformkit/policy/task.rego`, evaluated in process by `kit/tenancy/providers/opa`: assignment is allowed, an unassigned task may be resolved by any holder of `task:resolve`, and an assigned one only by its assignee. A refusal answers 403 `POLICY_DENIED` and is recorded as `security.denied` with the action, the rule's reason and the policy's revision (`sha256:` and twelve hex characters of the source), because `kit/httpx` hands every `tenancy.RequirePolicy` refusal of an authenticated request to the same audit hook as a missing grant.

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

`Deps.Gate` is a `rest.Gate`: the door [kit/rest](../kit/rest/README.md) asks at the
lock-merge-write seam of the generic PATCH, the DELETE and the three commands. The module
answers nothing about *which* fields are protected, and imports no peer module to ask: it
publishes the vocabulary, and the composition answers, with the installation's own switch.

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
