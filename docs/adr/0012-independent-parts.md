# 0012: Independent packages must simplify an adopted consumer

Status: accepted. The adoption-first direction is approved; implementation,
major-version migration and release qualification remain unfinished. This
supersedes the unmerged `functional-core` proposal numbered 0012, including its
local accepted status; it does not adopt `crud.Outcome`, `crud.Apply` or a
transaction-wide decision timestamp.

## Decision

A public package must solve a named problem outside PlatformKit and reduce the
code, dependencies or composition work of an existing consumer. Trace its
resolved dependency and execution path before choosing reuse, extending an owner
or extraction. Record that consumer's before/after result and ordinary versioned
Go consumption. A published package or synthetic example alone is not adoption.
[CONTRIBUTING](../../CONTRIBUTING.md#trace-an-existing-consumer) owns this review.

Keep one implementation. Existing names may alias or delegate to the new owner
when compatible; migrate the traced caller and remove the replaced logic in the
same dependent delivery. Defer an extraction that leaves two maintained copies
or adds layers without a consumer benefit. Do not split every domain command
into ports, projections and adapters. Task's existing SQL service and fake share
its pure domain decision; the owning transaction still locks, authorizes,
updates and records the event. Standalone Task resolution and module planning
extraction remain deferred until an adopted caller justifies them.

Packages are independently importable inside the existing Go module. Separate
Go modules are a later decision requiring independently managed versions and
consumers, not a consequence of creating directories. Optional SDKs belong at
explicit provider edges. Dependency checks cover transitive runtime imports;
application composition may depend on multiple edges. Do not duplicate a core,
add implicit provider registration or weaken a guard to claim independence.

## Schema and presentation ownership

Use the [existing entity and presentation owners](../../ARCHITECTURE.md#entity-and-presentation-contracts).
Entity metadata describes supported compiled fields; it is not a complete
semantic schema, field authorization policy or versioned collection definition.
The downstream catalog's Capture contracts own configurable definitions and
answer normalization. Immutable definition versions, publication and atomic
submission remain unfinished. Products retain reference checks, money/unit meaning, access and
business transitions. Project those definitions through the existing controls,
resource commands and component Props rather than adding a schema or component
registry. Source export and the native resource catalog do not establish A2UI
transport, data binding or action execution.

Before extending these contracts, prove one versioned collection flow with exact
values, conditional/repeated answers, access revocation and atomic submission,
then its second product consumer. Preserve old answers and schema identities.
This decision does not claim that those workflows or 999 product domains are
implemented or qualified for production load.

## Public compatibility and release

The module has a stable v1.0.0 tag. Preserve its public contracts within a stable
v1 release; deprecation documents a migration, not permission to remove an API.
Use `Deprecated:` comments with a working replacement and retain delegation
through the supported major line. Removing APIs or changing required interfaces
requires an explicitly planned major release and migration/support policy.
Go requires a `/v2` module/import path for a v2 release.
[Go's versioning guidance](https://go.dev/doc/modules/major-version) defines this boundary.

Current main already contains breaking changes after v1.0.0, including migration
and composition APIs. Before another stable tag, classify the complete API delta
and prepare a `/v2` module/import migration for the breaking rebuild. A v1 release
would instead require restoring and verifying v1 compatibility. This decision
does not rename the module or establish v1 support dates; those are release
prerequisites. Pseudo-versions and a clean-database rebuild do not establish compatibility.
[RELEASE](../../RELEASE.md) owns the approved publication procedure; this decision
neither selects a release date nor authorizes publication or deployment.

Compare exported APIs against the supported release using pinned tooling, and
review behavioral, wire and database compatibility separately. Type aliases may
need explicit compatibility review; a tool report is not an automatic exception.
[apidiff](https://pkg.go.dev/golang.org/x/exp/apidiff) approximates type compatibility
and cannot establish behavioral compatibility. Existing tests and real downstream
builds remain required evidence. Applied migrations stay immutable under
[ADR 0011](0011-migration-ownership.md); rolling upgrades require old/new process
compatibility evidence or an explicit stop-and-migrate operation.

## Evidence

Source-owned package examples and dependency gates establish the advertised
import boundaries. Product tests establish adoption and transaction behavior.
Use the [ordinary public-module proof](../../ui/forms/testdata/standalone/README.md)
and [pinned API comparison](../../scripts/PUBLIC-API.md). Commit verification
commands beside their owners; keep generated logs as CI artifacts identified by source revision. A workstation-only receipt,
passing import, or provider constructor does not prove deployed readiness.
