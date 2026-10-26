# Application boot and lifecycle

`kit/app` is where a composition becomes a process. `New` validates the plan and
builds the dependencies the modules were declared against; `Start` migrates,
opens the connection, the transport and the cache, mounts the routes and the
jobs, and returns the `Runtime` that `Run` serves and stops. `Bootstrap` and
`RunCommand` are the two doors outside that life: the first tenant of an
installation, and one operator command. `AsyncAPI` renders what the composition
declared, and the telemetry providers are installed here and nowhere else.

## What this package owns

The kernel's two cross-tenant verbs and the boot that declares them: the placement
walk below, the ledger move and the replay mounted on the control plane
(`ledger_ops.go`), the in-process stream hub (`stream.go`), and the one
`TracerProvider` and `MeterProvider` a process gets. Everything else in the kernel
reaches telemetry through the OpenTelemetry API and `kit/telemetry`'s vocabulary.

## Choosing the telemetry providers

Tracing and metrics are off when `telemetry.otlp_endpoint` is empty, and "off"
means *no provider is installed*: the global's default provider is already the
no-op one, so installing a no-op would buy nothing and would spend the one install
a process gets — which is what lets a test, or an application that embeds this
runtime, install a recorder of its own instead. The propagator is still installed,
because a process that exports nothing still carries the trace it was handed.

The install happens once per *started* application, at the end of `Start`, because
the half of it that survives is a fact about OpenTelemetry rather than a
preference: each global delegates to the provider installed *first*, and a later
install changes only what a provider lookup answers and the tracers made after it.
The kernel's tracers are taken as each span opens (`telemetry.Tracer`), so a span
answers to the provider installed when it opened; its meter was taken once, when
`kit/telemetry` was first used, so the instruments of the process answer to the
first provider installed here and to no later one. Either way a second install in
this package would buy a process two answers to one question and use the first —
which is why a composition that is refused gets no vote.

Installing is the one change a boot makes in its process that no release gives
back: the pool, the store, the transport and the event shapes a composition
declares all come back on a refused boot or a `Close`, and
`otel.SetTracerProvider` has no undo worth doing, because a "previous" provider is
only the last one somebody else installed. So it belongs where the boot stops
being refusable, and a plan (`telemetryPlan`) carries the decision from the gate
that reads the configuration to the call that installs it.

A resource describes the process, and this process serves many tenants, so no
tenant belongs in it, and no client either when the installation is shared: the
tenant arrives on the spans, where a request put it. Both providers merge
`resource.Environment()` into whatever they are handed, so a reserved key named in
`OTEL_RESOURCE_ATTRIBUTES` would reach every span and number this process exports
no matter what `resource` builds, and the only way to hold that promise with the
SDK as it is pinned would be to edit this process's environment behind its owner.
A reserved key named at boot therefore stops the boot, the way a
`telemetry.otlp_endpoint` that no exporter can use stops it: a configuration that
would attribute one tenant's data to a process that serves many is wrong in a way
no operator should discover in the backend, and the message says which key and what
to do instead. Every other key the environment names — a deployment environment, a
cluster, a pod — arrives unchanged.

## Placing a tenant under an app

Naming an app has two halves. The migration file
(`migrations/000060_tenant_app_place.up.sql`) and the standing step
(`placement.go`) are the same walk — `platformkit_place_tenants`, created by
`migrations/000059` — and they are two because a migration file is spent the
moment it drains. The upgrade that installs a release while the deployment still
names no app is a supported boot: it drains 000060 over rows it must not touch,
history marks the file applied, and the operator's later `nats.app` arrives to a
ledger that says the placement already happened. Without the standing step those
tenants keep the empty app they were stamped with — served, on the legacy durable,
outside both named apps, with `RelayApp` finding nothing for the composition that
has just named itself after them (`kit/events/relay.go`). Choosing an app name is a
deployment decision made after a release is installed, so the placement has to be a
deployment step, asked of every boot that declares an app.

What the step does not inherit is the refusal. `migrations/000060` refuses a boot
that named itself and left a tenant nobody named, which is right at the release,
where one statement sees every row and the operator is mid-deploy. It would be the
wrong answer here: a tenant created after the placement ran is a tenant whose app
its next deploy's declaration may simply not name yet, and a boot that died on it
would be the write that takes the last one away, held over a boot that came to
place somebody. So the walk runs, the tenants it cannot prove stay where they are,
and the fact is printed rather than decided.

Two of these run at once, and that is the shape of a server holding two apps over
one database: each composition places on its own boot and on its own tick, over the
same rows, with no knowledge of the other. The walk decides from a reading, so the
guarantee lives in the write it makes: `platformkit_place_tenants` sets the app of a
row only while that row is still empty at the write itself (`migrations/000059`),
which leaves the second of two competing placements to find the row already named
and write nothing. A tenant's app is decided once (`migrations/000043`) and which
relay, consumer and control-plane scope can reach the tenant follows from it, so the
step from empty to a slug is the one write in this kernel that may not arrive twice;
a declaration that finds a row already answered does not move the tenant and does
not report it as somebody's omission either — the late declaration is the one that
is wrong about the row, not the row about itself.

The step also declares the tenant's ledger key, in the schema rather than here
(`migrations/000061`'s trigger on the row whose app went from nobody's to
somebody's), and that is what makes two boots safe for each other's *moves* rather
than merely consistent about one row. `events.MoveLedger` holds that key exclusively
over every tenant that could still become its app: a placement that arrives first is
that move's ordinary contention refusal, and one that arrives after waits and lands
behind it, which is the order that owes a move — and the step runs before a boot
opens any scoped consumer, so its own next move pays its own debt.

Its own, and no further, because the boot that places is not the only one consuming.
A replica that has already moved this app's ledger and opened its scoped consumers is
consuming for this tenant the moment this write commits, whatever its own move
reported a minute earlier: the move it ran could only rename the claims of the
tenants that were its app's at the time, and this tenant was nobody's. What stands
between that consumer and a second handling of work this tenant already committed is
not the order of two boots' moves at all, but the claim's own reading of the
tenant's history under the durable it was marked under before this step named it
(`kit/events`'s claim). A placement therefore never has to wait for a rename it
cannot see coming, and the rename never has to happen first: the debt this step
leaves is one the next move finds and pays, and the deliveries in between refuse on
their own. What it costs is a placement sitting behind another app's rename: one
short transaction, the only holder it can ever wait behind.

## Composition

**Reused** — `pkit`'s plan, `db.Tx`, `httpx.Surfaces`, `events`' claim and
`MoveLedger`, `jobs`, `cache` and `config`'s loader remain the whole boot path;
this delivery added no unit to it. **Added** — `README.md`, because the four
sibling kernel packages (`kit/httpx`, `kit/events`, `kit/db`, `kit/rest`) each
carry one and this one had no documentation owner for prose that does not fit
above a function. **Made reusable** — the two sections above, "Choosing the
telemetry providers" and "Placing a tenant under an app", which the code now links
instead of retelling, so a later round on `kit/app` has somewhere to put the
argument rather than another comment block.
