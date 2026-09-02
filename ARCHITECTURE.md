# Architecture

PlatformKit is a reference architecture: a working multi-tenant SaaS whose value
is insight per line, so every idea below is implemented exactly once. It is
being extracted from a larger private codebase one stage at a time; anything
marked `(E1)`, `(E2)`, ... does not exist yet.

## The ten ideas

1. **Composition is a list.** An application is a slice of modules constructed in
   `apps/platformkit/main.go` with typed dependency structs in dependency order,
   so the compiler checks the wiring graph and there is no container to learn. (E2)
2. **A module is three things.** `contracts/` (interfaces, DTOs, events,
   permission tokens, and a conformance suite), `internal/` (every
   implementation), and `module.go` (the manifest). (E2)
3. **Cross-module dependencies are Go interfaces.** A consumer takes an
   interface declared in the provider's `contracts/`; `internal/` makes any
   other coupling a compile error. (E2)
4. **Tenant isolation belongs to the database, and to the type system.** Tenant
   tables carry `FORCE ROW LEVEL SECURITY` and the tenant is set per
   transaction, so a forgotten `WHERE tenant_id` returns nothing rather than
   another tenant's rows. `db.Tx[db.Tenant]` and `db.Tx[db.System]` are distinct
   types, so crossing the tenant by accident does not compile and crossing it on
   purpose is one grep. The settings themselves are `USERSET` and no privilege
   can withhold them, so the grep gate is what closes that door; the re-read
   before commit is a backstop that catches the careless escape and not the
   deliberate one. See `docs/adr/0003`. (E1)
5. **Authorization is declared, not remembered.** Every operation registers an
   `Auth` value alongside its route; the app validates the recorded operation set
   at boot and refuses to start when one is undeclared. (E1)
6. **Events leave through one door.** A module writes an event in the same
   transaction as its state change; one outbox relay publishes to JetStream. (E1)
7. **One migration directory, one ledger.** All SQL lives in `migrations/`,
   numbered once, applied by the owner role at startup or by `--role migrate`. (E1)
8. **Screens derive from schemas.** List, detail and form come from an entity's
   schema; a hand-written screen is an exception that has to earn itself. (E4)
9. **A client is configuration.** One image, one binary, `--role web|worker|all`;
   clients differ by configuration and assets, never by build. (E6)
10. **Invariants are gates.** Everything above is checked by a command that runs
    on every pull request in under five minutes; a rule that does not run is not
    a rule. (E0)

## Layout

| Directory | Holds | Stage |
|---|---|---|
| `kit/` | the kernel: db, tenancy, config, problem, httpx, module, crud, events, jobs, health | E1 |
| `modules/` | business modules, each `contracts/` + `internal/` + `module.go` | E2, E3, E5 |
| `ui/` | typed components, style engine, generated CRUD screens, htmx assets | E4 |
| `design/` | design tokens and theme resolution | E4 |
| `apps/` | `platformkit`, the reference binary | E2 |
| `tools/` | `locbudget`, the line-budget ratchet | E0 |
| `migrations/` | the one migration directory | E1 |
| `deploy/` | Dockerfile, Postgres bootstrap | E0 |
| `docs/adr/` | the decisions, ten files at most | E0 |

## Limits

Ceilings, not baselines. `loc-budget.json` ships with these numbers and the
ratchet only ever lowers them; raising one is an owner commit with a reason.

| Bucket | Ceiling (production lines) |
|---|---|
| `kit/` (kernel) | 15,000 |
| `modules/` | 150,000 |
| `ui/` + `design/` | 40,000 |
| `apps/` + `tools/` | 8,000 |
| total production Go | 250,000 |
| test Go | 250,000 |
| browser JavaScript | 6,000 |
| markdown | 20,000 |
| first-party packages linked into the app | 400 |

## Gates

`make check` runs gates 1 to 5, 7 and 8 today; 6, 9 and 10 arrive with the stage
that gives them something to check. The list stops at ten.

| # | Gate | Proves | Stage |
|---|---|---|---|
| 1 | `make build` | the wiring graph type-checks | E0 |
| 2 | `make vet` + `make fmt-check` | no known-bad and no unformatted Go | E0 |
| 3 | `make test` | the suite passes against a real Postgres | E0 |
| 4 | `make check-loc` | no bucket exceeds its ceiling | E0 |
| 5 | `make check-packages` | the app links ≤ 400 first-party packages | E2 |
| 6 | `scripts/check_imports.sh` | a module imports only another module's `contracts/` | E2 |
| 7 | boot validation test | no operation ships without an `Auth` declaration, and no route requires a permission no module defines | E1 |
| 8 | tenant isolation test + `make check-gucs` | RLS blocks cross-tenant reads as the app role, and only `kit/db` writes the tenancy settings | E1 |
| 9 | empty-database boot test | the app migrates and serves from nothing | E2 |
| 10 | one Playwright spec | the admin shell renders and a CRUD screen works | E2 |
