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

The stable **v1.1.0** tag (2026-09-18) is the public API contract, and it is what
every compatibility question is now measured against. It is also a break in the
line: v1.0.0 → v1.1.0 is not an upgrade path, published as a minor although
71 exported changes required a major, because no consumer sat on the v1.0.0 stable
line and the owner chose to move rather than revert the kernel boundary. The
reasoning, the apidiff evidence and the exact-pin warning are in
[CHANGELOG.md](CHANGELOG.md). Do not read that decision as precedent for the next
one: from v1.1.0 forward a breaking change requires a `/v2` module and import
migration under [Go's versioning rules](https://go.dev/doc/modules/major-version),
and a v1 release must restore and verify compatibility with v1.1.0. The `/v2`
migration remains unfinished; pseudo-versions do not establish compatibility.

Use `Deprecated:` comments with working replacements and retain compatible
delegation through the supported major line — knowing what they cannot do. An
alias repairs a *removal*; it does not repair a *move*, because apidiff resolves
the alias and still reports the change, which is how the 29 moved types in
v1.1.0 came to be unrepairable. Before a major release, record the
complete API review, consumer migration instructions and v1 support/security-fix
policy, including supported versions and dates. Run the [pinned API comparison](scripts/PUBLIC-API.md)
on committed revisions and resolve its report before release. Review aliases,
function/interface changes, wire, behavioral and database compatibility with real
consumers; a type report alone cannot establish compatibility.

A line that comes to carry breaking changes somebody will not revert may record
them with `--baseline` and `--write-baseline` rather than leaving the report
permanently red; v1.1.0 needed that file and retired it, and carries no accepted
break from itself. A baseline that cannot go stale is a list of excuses, and
publishing the next stable release ends it: measure against the new tag rather
than carrying a list forward

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
**An unread result is not a green one.** Read the Gitea Actions run for that
commit and its conclusion before tagging; a tag is what an outside consumer
trusts, and "the job was picked up" is not evidence about its outcome. v1.1.0 was
tagged with this verdict unread and the run had failed, on a step no local
aggregate runs — see the browser assertions in
[tools/designexport/openpencil](tools/designexport/openpencil/README.md). If the
verdict cannot be read, the release is not ready.
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
