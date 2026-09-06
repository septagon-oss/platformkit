# 0012: A command is a pure decision, applied

Status: accepted; the first modules on it are content and site, the rest follow.

## Problem

Each command mixed its rule with its effects: a service read a row, changed it
in place, read the wall clock (`db.Now()`) and wrote the row and the event. The
same rule was then mirrored by hand in the module's fake, and a conformance
suite was the only thing keeping the two copies equal. GORM stamped
`created_at` and `updated_at` from its own clock, so one transaction wrote three
different "nows", and a test could only assert a window around its own clock.

## Decision

A command's rule is a pure function in the module's `contracts/`: it takes the
entity as it is and the instant the command runs — and any other effect, such
as a fresh id, as an argument — and answers a `crud.Outcome`: the entity as it
is next, and the event that announces the change, or no event when nothing
changed. It touches no database, no clock and no caller's copy; the same
arguments always answer the same way.

The transaction carries the instant. `db.Tx.At()` is the moment it opened, the
handle's clock is pinned to it, so every timestamp written inside — rows,
events, decisions — is that one instant. `crud.Apply` is the effectful half:
read, decide, and when the decision moved the row, write the named columns and
publish the event in the same transaction; otherwise write and say nothing.

A module's fake applies the same decision to memory. There is one copy of every
rule; the conformance suite checks the two shells, not two rules.

## Consequences

* Rules are tested as functions, with chosen instants and no database; the
  shells are tested once, in `kit/crud`, for every module.
* A repeated command is silent by construction, not by a check in each service.
* `db.Now()` remains for what runs outside a transaction; inside one, `tx.At()`
  is the clock, and a service that reads `db.Now()` is the pattern this replaces.
* Two updates in one transaction carry the same `updated_at`, which is what a
  transaction means; the weak ETag on a public page changes per transaction.

## Evidence

`go test ./kit/crud ./modules/content/... ./modules/site/...` — the kernel's
Apply test, the pure decision tests, and the conformance suites against the
fake and against Postgres.
