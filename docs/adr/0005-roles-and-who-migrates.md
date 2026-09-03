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

## Consequences

- Deploying is `kubectl set image` on two deployments of the same image.
- A worker cannot serve a stale schema: it applied the schema itself.
- A migration that is slow makes every replica's boot slow, which is visible in
  the rollout rather than hidden in a job that finished an hour ago.
- Backwards-compatible migrations are not optional. Old and new code run at once
  during a rolling deploy, both against the newest schema.
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
```
