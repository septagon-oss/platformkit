# 5. One binary, three roles, and every role migrates

Status: accepted, 2026-09-02

## Context

The previous deployment had a web image, a worker image and a migration job,
built from one tree but shipped as three artifacts, plus an init container whose
only purpose was to make the other two wait. Most incidents in that sequence
were ordering: a worker that started before the migration, a migration job that
had already run, a rollback that left the two images on different schemas.

## Decision

One binary and one image. `--role web|worker|all` chooses what the process does:

- **web** builds the API, runs the boot gates and serves.
- **worker** relays the outbox, consumes subscriptions, runs the periodic jobs,
  and serves exactly two routes — `/health` and `/ready` — on the same address,
  so one orchestrator manifest describes both roles.
- **all** is both in one process, with the in-memory transport, which is what a
  laptop and a small deployment want. It is the default.

**Every role runs `Migrate` at boot.** `db.Migrate` takes a fixed advisory lock
before it touches the ledger, so several processes racing to migrate is one
process migrating and the rest waiting and finding nothing to do. That removes
the ordering problem instead of sequencing it: there is no migration job to run
first and nothing to wait for.

One thing is the worker's alone. A `phase=data` migration over a table that already has
readers is drained by `jobs.BackfillMigrations`, which the worker runs on a tick, because
holding a boot open for the length of a ten-million-row scan is not what the boot above
is for; `Migrate` stops in front of it and returns success. See
[ADR 0011](0011-migration-ownership.md).

And one door is the operator's. `platformkit migrate` composes the same sources, the
same floors and the same budgets as the boot above (`app.Migrate` is that composition
once) and then exits, for the case the boot cannot serve: a migration that came back
*contended* — it declined to wait for a lock past its budget — may be run again by
whoever decided to wait, and deciding that is not a deployment. `--drain` finishes a
backfill instead of waiting for the tick.

## Consequences

- Deploying is `kubectl set image` on two deployments of the same image.
- A worker cannot serve a stale schema: it applied the schema itself.
- A migration that is slow makes every replica's boot slow, which is visible in
  the rollout rather than hidden in a job that finished an hour ago.
- This rebuild starts on a fresh database, with no old-ledger conversion.
  Later releases must choose a rollout explicitly: expand the schema for
  overlapping binaries, or stop the old processes before a breaking change.
  An old binary cannot restart with an incomplete migration history.
- The probes are a plain mux in both roles, not the API: a probe has no tenant,
  no session and no operation to declare, so it has no business building one.
  The web role mounts the same mux beside the API, outside the middleware chain,
  because the chain resolves a tenant from the request host before anything else
  runs — a query with a two second budget — and liveness that waits on it fails
  during the outage it exists to survive. One handler, one readiness body, and
  an operator does not have to learn it twice.
- Every role runs the boot gates, and the roles that do not serve discard the
  router. A worker that skipped them would start on a composition the web role
  refuses — the same image, the same modules, two answers — and the rollout
  would look half healthy.

## Evidence

```sh
go test ./kit/app -run 'TestWorkerRelaysAndAnswersItsProbes'
go test ./kit/db  -run 'TestMigrateIsIdempotent'
go test ./apps/platformkit -run 'TestMigrateCommandAppliesTheComposedSources'
```
