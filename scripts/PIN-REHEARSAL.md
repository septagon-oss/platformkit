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

One receipt says one revision was rehearsed; a release is not published from one.
Installations sit at the oldest tag of the line and at its later patches, and which
those are is a policy question that `docs/COMPATIBILITY.md` and `RELEASE.md` answer.
`scripts/testdata/rehearse/supported-sources.json` is the same answer at the resolution
a gate needs: the commit each name is, the obligation it discharges, the revisions
refused with a reason, the fixture files the cases must seed, and the applied migration
bytes an installation already holds and compares whole.

`scripts/supported_sources.sh` is its only reader, and checks what a record gets wrong
silently: every ref resolves here to the commit beside it, every required obligation is
discharged once and states why it is one (the verdict quotes that reason), no proof build is
called supported (`docs/COMPATIBILITY.md` gives it no claim), the files owed hash to what is
owed, and no source is refused by being left out. It also refuses a record that answers
*less* than this repository already did. The floor is not the record: it is the stable tags
of the release line being broken from, plus every revision a committed revision of the
record called supported. Each must be answered — supported, or refused with a reason.
A release may widen the promise in this file as it likes; it narrows it only where a policy
document refuses the revision, because a case deleted beside the obligation it discharged
is self-consistent by construction and would otherwise be certified as the whole promise.

`./scripts/rehearse_migrations.sh --supported` runs the same step once per supported
source, in `scripts/rehearse_supported_matrix.sh`, and writes one
`platformkit.rehearsal.v2` receipt: a case per source, each refused source's refusal as
the record answered it (the record is asked, per source, and the answer and its exit code
are what the receipt shows), and the exported-surface comparison's verdict beside the data.
It refuses `--base-ref`, `--dump`, `--seed` and an environment that replaces either fixture
beside it: a receipt for one case, or for one case's substitutes, certifies only that.

An empty schema is not evidence that data survived. Each case seeds
`scripts/testdata/rehearse/seed.sql` for volume and `populated.sql` beside it for a
second tenant, page text, file records and queued events, then retains the ledger and
`scripts/rehearse_facts.sh`'s facts *before* the candidate connects and requires the
same list after: per-tenant digests of body text, file hash and storage key, outbox
payload and task state, the pending queue, the tables that still force their policy,
and how many rows of every other tenant each tenant's own session sees, read as the
application role — the owner role answers that last question with "everything".
`FACTS CHANGED`, `TENANT LEAK` or `AUTHORITY LOST` is exit 1.

Those two reads are kept, not deleted with the run: a passing case copies the ledger and
the facts, before and after, into a directory beside its receipt (`${REHEARSE_RECEIPT}.d`,
or `REHEARSE_EVIDENCE` where the matrix gives each case its own), names the four paths in
the receipt's `evidence`, and prints the directory. A release reviewer inspects the
pre-upgrade ledger the promise is measured against, and
`scripts/check_pin_rehearsal.sh --strict` re-takes every figure below from those bytes, so
a receipt that lost its evidence, or disagrees with it, is refused rather than quoted.

`./scripts/check_pin_rehearsal.sh --strict` reads the matrix and reads the record for
itself: a missing case, a repeated case, a drifted revision, another support record, a
case that seeded a fixture the record does not owe, a matrix that recorded no
exported-surface comparison, or a single-revision receipt is refused even when every
case inside passed. The data figures are obligations at that read as well, and not a
number beside each case: at least one tenant-boundary read, zero rows of another tenant
visible to the application role, an applied ledger the case arrived with, and every one of
those rows retained. The producer refuses the same shapes as it runs; the release is
certified where the receipt is read, so the reading happens on both sides — and the figures
themselves are re-taken from the evidence named above, not read out of the run that wrote
them. `REHEARSE_ARGS=--supported
REHEARSE_RECEIPT=$PWD/.artifacts/rehearsal.json make check-rehearse` is the release
form; the default stays the single-source diagnostic it was.

## What this repository owes and what the consumer owes

Here: the rehearsal runs in `make check` (`check-rehearse`, one source, and
`check-rehearse-supported`, the whole matrix, in the merge tier and in CI), the receipt
is written, this script exists and is tested here, and the public-API comparison
runs as `check-apidiff` against the release line's frozen surface, named by the
line record under `scripts/baselines/`. Wiring the pin check into a client's own pipeline is that
client's change, exactly as `scripts/check_imports.sh` is called from a consumer's
CI rather than from this repository's.
