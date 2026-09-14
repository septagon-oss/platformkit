# Task policy example

This opt-in Rego policy demonstrates the facts sent by
[`topaz.New`](../../../kit/tenancy/providers/topaz/topaz.go). For the reference
task lifecycle, it allows a signed-in member to claim unassigned work for
themselves and resolve their own assigned work. Assignment and resolution retries
remain subject to the task service's existing idempotency and conflict rules.

The application composes the policy client with path `platformkit.task`, decision
`allowed`, and the revision of the policy artifact it deploys. This directory
does not activate a policy or configure a Topaz server. The application retains
route permissions, tenant isolation, account checks and domain transition rules.

In the application's existing composition, import
`github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz` and construct
`topaz.New` from `authorizer.NewAuthorizerClient(conn)` and
`topaz.Options{Path: "platformkit.task", Revision: deployedRevision}`.
`conn` is an application-owned gRPC connection with transport credentials
appropriate to its Topaz deployment; the adapter neither opens nor closes it.
Handle constructor errors at startup.
Pass the resulting policy to `task.NewServiceWithPolicy`, then supply that same
service through `task.Deps.Service` and to every product module calling tasks.
For a composition without shared task callers, `task.Deps.Policy` constructs the
service inside the module. Supplying both dependencies is rejected.

Assignment and resolution load and lock the tenant's task before deciding,
including idempotent retries. Their actor comes from the authenticated principal;
absent principals are refused, including callers that enter through the service
instead of HTTP. The resource carries current status, priority and assignee;
assignment separately supplies the requested assignee. Existing domain checks
still run after an allow. The SLA sweep, CRUD operations, list scoping and command
discovery retain their existing behavior; this example does not authorize those
through Topaz. The reference app does not enable the external provider by default.

Run the examples with the official OPA CLI, version 1.15.2, from the foundation
repository root:

```sh
opa test modules/auth/policies -v
```

The named cases cover two tenants, ownership, actor kinds, invalid state,
assignment and resolution retries, and nested attributes that must not override
the trusted envelope. Go protocol tests separately verify that the adapter sends
this envelope to the official Topaz gRPC API:

```sh
go test ./kit/tenancy/providers/topaz -run '^TestTopaz' -count=1
```

`input.resource` contains `tenant`, `actor`, `action`, and `resource`. Object
attributes live at `input.resource.resource.attributes`. User and system actors
use Topaz's manual identity mode; public actors use its no-identity mode. This
example reads the trusted envelope and requires no directory seed. A policy
that calls directory functions still requires an application-owned, reliably
synchronized relationship model. No directory synchronization or atomicity
between Topaz and PostgreSQL is supplied by this example.
