# 9. Public foundation and private consumers

Status: accepted, 2026-09-03. Migration ownership is defined by
[ADR 0011](0011-migration-ownership.md). This record refines the public
repository boundary in [ADR 0001](0001-one-repository.md).

## Context

The foundation needs to build and run without private source or credentials.
Commercial capabilities and client configuration have different owners and
access requirements. Keeping shared implementation in one public module does
not require publishing those consumers.

## Decision

The public repository contains `kit/`, `design/`, `ui/`, reference business
modules and the reference application. A private catalog supplies commercial
capabilities. A private application composes the public foundation, selected
catalog capabilities and client modules with its configuration and assets.

The dependency direction is application → catalog → public foundation.
Modules cross boundaries through public contracts, not another module's
implementation. The public repository never imports or requires private code.
Compatibility with a private consumer is tested in that consumer's repository.

Public documentation describes the consumer seam without naming private
repositories, catalog capabilities, clients or infrastructure. Client data,
private source, cluster state and credentials do not belong in the public
tree. Example tenants and configuration must not expose a customer's identity
or deployment. Publishing source makes its history public; removing it later
does not make that disclosure reversible.

## Dependency and migration contracts

A releasable consumer pins a published version of the public module. During
development, its `go.mod` may select a sibling checkout through `replace`;
that dependency must be explicit and available wherever the development build
runs. A successful local build with a replacement does not validate the version
named in `require`. Before publishing, test the selected published dependency
without a machine-local path. The consumer's release procedure owns that check.

The foundation and each selected module provide independent migration sources
to one runner in composition order. The module name identifies its history
owner; versions increase within that owner. There are no global repository
ranges. Applied history is immutable, as specified in ADR 0011.

## Review and evidence

Current source and package ceilings live in
[loc-budget.json](../../loc-budget.json) and
[packages-budget.json](../../packages-budget.json). Do not maintain a second
table of their values here. `make check-loc` and `make check-packages` verify
the checked-out source; [CONTRIBUTING.md](../../CONTRIBUTING.md) defines the
separate owner review needed for a justified increase.

The public application's build, tests and browser checks must work without
private modules. Its dependency declarations are in [go.mod](../../go.mod);
module-boundary enforcement is in
[scripts/check_imports.sh](../../scripts/check_imports.sh).
Consumer compatibility remains a separate test obligation. A public release
does not certify a private product's deployment or user journeys.
