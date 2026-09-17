# Release PlatformKit

A release publishes a reviewed commit from `main` as a versioned Go module,
container image and release notes. Publishing requires the repository owner's
authorization. This page describes preparation and publication evidence; it
does not provision infrastructure or migrate a production database.

GitHub Actions publication is currently disabled. The retained
[release workflow](.github/workflows/release.yml) does not run merely because
its file exists. The active [verification workflow](.gitea/workflows/ci.yml)
tests source and local editor images without publishing. Before creating a
release, agree its publication mechanism and destinations with the owner;
do not enable automation or grant runners publication credentials implicitly.

## Choose the compatible release line

The stable v1.0.0 tag establishes a public API contract. Current development has
breaking changes, so the next stable breaking release requires a `/v2` module
and import migration under [Go's versioning rules](https://go.dev/doc/modules/major-version).
A v1 release must instead restore and verify v1 compatibility. The migration
remains unfinished; pseudo-versions do not establish compatibility.

Use `Deprecated:` comments with working replacements and retain compatible
delegation through the supported major line. Before a major release, record the
complete API review, consumer migration instructions and v1 support/security-fix
policy, including supported versions and dates. Run the [pinned API comparison](scripts/PUBLIC-API.md)
on committed revisions and resolve its report before release. Review aliases,
function/interface changes, wire, behavioral and database compatibility with real
consumers; a type report alone cannot establish compatibility.

The changes already accepted on this line are enumerated in
[PUBLIC-API-BASELINE.json](scripts/PUBLIC-API-BASELINE.json), so a new break is
distinguishable from the ones already reviewed. Publishing a stable release ends
that file: measure against the new tag and delete it, rather than carrying a list
of excuses into the next line. Restoring v1 compatibility instead of releasing
`/v2` means the same file shrinking line by line, which is how you would know it
was working.

When the source migration is authorized, update imports, package guards and
proofs together, then foundation, catalog and clients in dependency order.
Prove each advertised public leaf with the [ordinary versioned consumer](ui/forms/testdata/standalone/README.md)
after its candidate version is publicly resolvable. Retain the receipt and
consumer revision with the release evidence.

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
Verify the active CI result for the exact release commit. An absent GitHub
check is not a passing result, and pushing a tag does not rerun these gates.
Editor artifacts also require the native, browser and built-editor checks in
the [design tooling guide](tools/designexport/openpencil/README.md).

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

Confirm the release commit has reached `main`, check it out with a clean tree
and choose the reviewed version. Inspect the actual push destinations before
publishing. The following uses `v2.0.0` only as an example after the major-version migration
and release review have completed:

```sh
git remote get-url --push --all origin
git tag -s v2.0.0 -m "PlatformKit 2.0.0"
git push origin refs/tags/v2.0.0
```

Use an annotated tag with `-a` only if the owner explicitly chooses an unsigned
release. Push the selected tag, not every local tag. Do not move or replace a
published tag; publish a new version for a correction. Repository rulesets and
signing configuration are external state and must be checked by the owner.
Verify the tag and its peeled commit at each intended destination. A partial
multi-destination push is not a synchronized release. Pushing the tag exposes
the Go module version; it does not publish the container or release assets.

## Verify publication

Use the explicitly approved publisher to build [deploy/Dockerfile](deploy/Dockerfile)
from the verified commit and publish to the agreed registry. The retained
workflow documents the former publisher, not evidence that an image exists.
Keep prereleases separate from `latest` and include release notes, the exact
source commit, immutable image digest and an SPDX SBOM of the published image.

Verify those artifacts at their destinations and confirm their source matches
the reviewed tag. Check image and module access from the intended consumer's
context; repository visibility alone does not establish artifact access.
Until the publisher is approved and these checks pass, publication is unfinished.

## Update consumers

Consumers update their dependency in their own repositories, run their gates
and release in dependency order. A local `replace` can hide a stale version pin,
so verify the module actually selected before publishing a consumer.

Deployment is a separate, authorized change to the environment's desired state.
Verify its pinned image digest, migration result, workload health and product
journey after reconciliation. A published release does not prove any environment
has adopted it.
