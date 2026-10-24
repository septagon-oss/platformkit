# Pin rehearsal

`scripts/rehearse_migrations.sh` applies this tree's pending migrations to a copy
of the previous release's database and reports what each file cost. ADR 0011 makes
it the step a version may not be published without, because the runner's rule
table says which statements are refused and nothing says what the release will
cost on a table the size an installation actually has.

This file is about the other half: the consumer that pins
`github.com/septagon-oss/platformkit`. On the day it bumps the pin, that
repository runs *this* module's migrations against *its* data. That is exactly the
moment the rehearsal was for, and a bump that skips it is a release nobody
measured. So there is a receipt, and a script that refuses a pin without one.

## The receipt

`scripts/rehearse_migrations.sh` writes it, as its last act, and only on the path
where it is true: `REHEARSE_RECEIPT` names the file, and no receipt is written for
a rehearsal that failed, refused, or could not sample its own lock waits.

```json
{"base_ref":"v1.1.0","base_commit":"893cf2ae…","candidate_commit":"594deab…",
 "seed":"scripts/testdata/rehearse/seed.sql",
 "files":[{"owner":"audit","version":"35","name":"000035_audit_context.up.sql",
           "phase":"expand","seconds":0.002}],
 "max_lock_ms":0,"lock_samples":9,"tool":"scripts/rehearse_migrations.sh","exit":0}
```

`max_lock_ms` belongs to the run, not to a file: the watcher samples the whole
rehearsal every 100 ms and cannot attribute a wait to one statement, so the
receipt says the one thing that was measured and not the thing that would look
complete.

## The refusal

`scripts/check_pin_rehearsal.sh` reads the pin out of the caller's `go.mod`,
resolves its commit from the module cache (`@v/<version>.info`'s `Origin.Hash`, or
`REHEARSE_PIN_REVISION` for a build that resolved it another way), and refuses —
exit 1, with the command that would have produced the receipt spelled out — unless
a receipt names that commit, was taken against a release tag, and records exit 0.

```
RECEIPT: REHEARSE_RECEIPT=.artifacts/rehearsal.json
STEP:    ./scripts/check_pin_rehearsal.sh --previous-release v1.1.0
```

Four verdicts, each a case in `scripts/check_pin_rehearsal_test.sh`, which `make
check` runs: a pin with no receipt; a receipt for another revision; a receipt not
taken against a release; a receipt whose rehearsal did not pass. A consumer with
no such pin in its `go.mod` exits 0 — there is nothing to have rehearsed — and a
check that cannot resolve the pin exits 2 saying what it needed.

The script has no `--skip`, and `make check` has no variable that drops
`check-rehearse`. A gate with a flag that turns it off is a note.

## The matrix: every installation the release promises

One receipt says one revision was rehearsed. A release is not published from one
revision: installations sit at the oldest tag of the line, at its later patches, and
at the revision whose `audit/000048` bytes are already applied. Which those are is a
policy question — `docs/COMPATIBILITY.md` and `RELEASE.md` answer it — and
`scripts/testdata/rehearse/supported-sources.json` is the same answer at the
resolution a gate needs: the exact commit each name is, the obligation it discharges,
and the revisions refused with a reason.

`scripts/supported_sources.sh` is its only reader. It checks what a record can get
wrong silently: that every ref resolves here to the commit beside it, that every
required obligation is discharged exactly once, that no proof build is called
supported (`docs/COMPATIBILITY.md` gives it no claim to make), and that no source is
refused by being left out. `--check-source REF` asks the question for one revision and
touches no database.

`./scripts/rehearse_migrations.sh --supported` then runs the rehearsal once per
supported source — its own copy, its own seed, its own drain, its own budgets — and
writes one `platformkit.rehearsal.v2` receipt with a case per source plus each refused
source's refusal, which it exercises rather than assumes. It refuses `--base-ref`,
`--dump` and `--seed` beside it: a receipt for one case certifies one case. Each case
also retains the ledger rows and the seeded facts *before* the candidate connects and
requires them after, so "the data survived" is a comparison against a snapshot the
upgrade cannot rewrite.

`./scripts/check_pin_rehearsal.sh --strict` reads the matrix, and reads the record for
itself: a missing case, a repeated case, a drifted revision, another support record or
a single-revision receipt is refused even when every case inside passed. `REHEARSE_ARGS=--supported
REHEARSE_RECEIPT=$PWD/.artifacts/rehearsal.json make check-rehearse` is the release
line's form; the default `make rehearse` and `make check-rehearse` stay the
single-source diagnostic they were.

## What this repository owes and what the consumer owes

Here: the rehearsal runs in `make check` (`check-rehearse`) and in CI, the receipt
is written, this script exists and is tested here, and the public-API comparison
runs as `check-apidiff` against the release line's frozen surface, named by the
line record under `scripts/baselines/`. Wiring the pin check into a client's own pipeline is that
client's change, exactly as `scripts/check_imports.sh` is called from a consumer's
CI rather than from this repository's.
