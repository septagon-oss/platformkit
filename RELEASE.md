# Release PlatformKit

A release publishes a reviewed commit from `main` as a versioned Go module,
container image and GitHub release. Publishing requires the repository owner's
authorization. This page describes the repeatable release procedure; it does
not provision infrastructure or migrate a production database.

## Prepare the release

Work in this repository with a clean tree. Review the changes, dependency
updates, migration compatibility and [CHANGELOG.md](CHANGELOG.md).
Use the configured development PostgreSQL and NATS services for verification:

```sh
git status --short --branch
make check
make e2e
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

A failed or unavailable check is not a pass. Resolve vulnerability findings or
record a reviewed assessment of reachability and mitigation before publishing.
The CI workflow runs vulnerability analysis; the release workflow currently
runs `make check` and `make e2e`, so do not assume the tag repeats every CI step.

Keep applied migration files unchanged. For a schema change, verify the upgrade
path and decide whether old processes can remain running. Follow
[ADR 0011](docs/adr/0011-migration-ownership.md); downgrading the image does not
undo applied SQL.

Review third-party provenance in [NOTICE](NOTICE) and preserve required
attribution. Do not strip a license header merely to satisfy a documentation
check. Keep secrets, local configuration and generated files out of the release.

Budget ceilings are reviewed separately from implementation changes.
`go run ./tools/locbudget --write` lowers them; any intentional increase needs
the separate owner review described in [CONTRIBUTING.md](CONTRIBUTING.md).
The actual source and package checks are already part of `make check`.

## Publish an approved version

Confirm the release commit has reached `main` and choose the reviewed version.
The following uses `v1.2.3` as an example, not as the next release:

```sh
git tag -s v1.2.3 -m "PlatformKit 1.2.3"
git push origin refs/tags/v1.2.3
```

Use an annotated tag with `-a` only if the owner explicitly chooses an unsigned
release. Push the selected tag, not every local tag. Do not move or replace a
published tag; publish a new version for a correction. Repository rulesets and
signing configuration are external state and must be checked by the owner.

## Verify publication

[.github/workflows/release.yml](.github/workflows/release.yml) is authoritative.
It rejects a tag outside `main`'s history and tests the tagged tree before
building [deploy/Dockerfile](deploy/Dockerfile). It pushes to
`ghcr.io/septagon-oss/platformkit`, attaches an SPDX SBOM and records the image
digest in the GitHub release. A prerelease tag does not advance `latest`.

Confirm the workflow succeeded, the intended tag and commit match, and the
release contains the expected image digest and SBOM. Check package visibility
from the intended consumer's access context; repository visibility alone does
not establish image access.

## Update consumers

Consumers update their dependency in their own repositories, run their gates
and release in dependency order. A local `replace` can hide a stale version pin,
so verify the module actually selected before publishing a consumer.

Deployment is a separate, authorized change to the environment's desired state.
Verify its pinned image digest, migration result, workload health and product
journey after reconciliation. A published release does not prove any environment
has adopted it.
