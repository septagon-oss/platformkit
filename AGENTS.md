# Working in this repository

Read `ARCHITECTURE.md` first: it names the ten ideas, the layout, the ceilings
and the gates. `docs/adr/` says why. Nothing else is required context.

## Rules

1. **Net lines go down.** `make check-loc` must pass. `go run ./tools/locbudget
   --write` only lowers a ceiling; raising one is an owner commit with a reason.
   A ceiling is the tree plus the smallest round margin — the count rounded up
   to the next hundred, which `--write --round 100` writes — and a tag
   re-ratchets. A ceiling at exactly the count is a ceiling the next one-line
   pull request fails, and a margin nobody can argue about is worth more than a
   tight one.
2. **No new channel.** No new registry type, config key namespace or generated
   document. Fourteen make targets, ten gates, one config surface.
3. **One task, one commit, green build.** `make check` passes before you commit.
4. **Delete the old path in the same commit that lands the new one.** No shim
   outlives its task; if the deletion cannot fit, the task is wrong — split it.
5. **Do not widen scope.** A defect outside the task is one line in an issue,
   not a fix in this commit.
6. **Verify before claiming.** Paste the real command output into the commit body.
7. **Never commit secrets**, `config.yaml`, binaries or generated assets.
8. **Close duplicates, never add them.** A second implementation of anything
   already here is rejected at review; an interface is justified by a passing
   fake, not by a second production implementation.
9. **Contracts before implementations.** A module's `contracts/` package and its
   conformance suite exist and pass before `internal/` is filled.

## Commands

    make up               # Postgres and NATS, waiting for both
    make check            # build, vet, fmt-check, test, check-loc,
                          #   check-packages, check-gucs
    make run              # the reference app, on config.yaml
