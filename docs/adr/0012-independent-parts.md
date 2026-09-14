# 0012: Independent packages must simplify an adopted consumer

Status: accepted. Implementation, major-version migration and release
qualification remain unfinished. This supersedes the unmerged `functional-core`
proposal numbered 0012, including its local accepted status; it does not adopt
`crud.Outcome`, `crud.Apply` or a transaction-wide decision timestamp.

## Decision

Choose adoption before extraction so independently usable packages reduce the
cost of building and maintaining real products. [CONTRIBUTING](../../CONTRIBUTING.md#trace-an-existing-consumer)
owns the consumer trace, reuse comparison and evidence required for extraction.

Task's existing SQL service and fake share its pure domain decision; the owning
transaction still locks, authorizes, updates and records the event. Standalone
Task resolution and module planning extraction remain deferred until an adopted
caller justifies them. Splitting each command into more layers would add a
maintenance cost without an established consumer benefit.

Packages are independently importable inside the existing Go module. Separate
modules require independently managed versions and consumers. Optional SDKs stay
at explicit provider edges, without implicit registration. Dependency checks
cover transitive runtime imports; application composition may join those edges.

## Schema and presentation ownership

Use the [existing entity and presentation owners](../../ARCHITECTURE.md#entity-and-presentation-contracts).
Entity metadata describes compiled fields, not a semantic schema, field
authorization policy or versioned collection definition. The downstream
catalog's Capture contracts own configurable definitions and answer normalization;
immutable versions, publication and atomic submission remain unfinished.
Products retain reference checks, money/unit meaning, access and business
transitions. Use the existing controls, resource commands and component Props.
Source export and the native resource catalog do not establish A2UI transport,
data binding or action execution.

Before extending these contracts, prove one versioned collection flow with exact
values, conditional/repeated answers, access revocation and atomic submission,
then its second product consumer. Preserve old answers and schema identities.

## Compatibility and evidence

[RELEASE](../../RELEASE.md#choose-the-compatible-release-line) owns public versioning,
deprecation and release prerequisites; [ADR 0011](0011-migration-ownership.md)
owns migration compatibility. The [public-module proof](../../ui/forms/testdata/standalone/README.md)
and [API comparison](../../scripts/PUBLIC-API.md) establish import and type evidence.
Product tests must establish adoption and transaction behavior; this decision
does not qualify the planned collection workflows or 999 domains for production.
