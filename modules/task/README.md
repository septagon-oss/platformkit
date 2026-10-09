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

Compose it with `task.Deps{Service, Policy, Tenants, SweepEvery}`; `Policy` is
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

### Public faces

None. `git grep` finds no `httpx.Public()` in `modules/task`, and every route is guarded by `task:read` or `task:update`. The module uses no `kit/limit`.

### The operator boundary

None. No permission in `permissions` sets `Operator: true`, and `spec` sets no `OperatorRead` or `OperatorWrite`. The routes are scoped to the caller's own tenant transaction.

### Provisioning

The module does not create roles. A composition grants `task:read` and `task:update` through the tenant's roles, using the auth module's roles API (see `modules/auth/README.md`). The administrator role holds the wildcard `*`, which satisfies these two keys (`docs/adr/0006-system-access-is-a-token.md`). The code does not name any other persona. To use resource decisions, pass a `tenancy.Policy` in `task.Deps.Policy`. `modules/auth/policies` has a Topaz adapter.

## Composition

**Reused** — the reading vocabulary `kit/entity` froze (`EntryHints`,
`FieldHints`, `CommandHints`, `Icons`, `Tones`) declared at the three sites
`kit/rest` already exposes (`Spec.Present`, the `ui:`/`enumLabels`/`enumTones`
tags in `derive`'s grammar, `rest.CommandOptions.Present`), the copy machinery
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
