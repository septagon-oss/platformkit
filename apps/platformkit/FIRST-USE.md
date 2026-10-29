# First use, measured

This is the receipt for [the README's one command](../../README.md#try-it-locally).
It exists because an installation promise that has no numbers behind it is a
description, and because the parts of a first run that are not this program's
fault — the module download, the toolchain, the PostgreSQL download, every attempt
that failed — are the parts a stranger actually spends.

`scripts/first_use.sh` takes the measurements and writes one JSON object per line,
into the file named by `--out` or `FIRST_USE_OUT`:

```sh
scripts/first_use.sh --samples 3 --out /tmp/first-use.jsonl
scripts/first_use.sh --samples 1 --published --out /tmp/first-use-published.jsonl
```

Each sample gets its own `HOME`, so each one pays for the module graph, the
toolchain, the compile and the database download from nothing; the failed attempts
are timed and reported beside the successful ones. It records `go`, the revision,
the version the public `@main` command resolves to, the bytes downloaded, the disk
each phase left, and the four durations:

| Field | What it times |
| --- | --- |
| `fetch_ms` | `go install` of the reference application in a home that has never seen it — module download, toolchain download when the home's Go is older than `go.mod`'s `toolchain` line, and the compile |
| `cold_ms` | the first `start` in that home, until `/health` answers 200 — the database download, its extract, `initdb`, the migrations and the bootstrap |
| `first_action_ms` | sign in, create a task through `POST /api/v1/task/tasks`, read it back |
| `warm_ms` | the same command again over the same `--data`, until `/health` answers 200, with the task still readable and no second password printed |
| `attempts[]` | every failed attempt, timed: an occupied `--addr` and an unwritable `--data` |

The figure the acceptance target is written against is
`fetch_ms + cold_ms + first_action_ms`, carried on every receipt as
`cold_plus_first_ms` — the whole public action from an empty home to a persisted
task. The failed attempts are reported **beside** that figure, in `attempts`, timed
like everything else and never added into it: they are time the person spent, and
they belong in the row that says which refusal cost them rather than in a total that
makes the two indistinguishable.

## The rules a receipt obeys

* A receipt is written only by the path that makes it true. A sample that refused,
  was interrupted, or could not read its own timings writes nothing, so no receipt
  in this file describes a run that did not happen.
* `attempts` is never empty, because the harness runs two failing attempts on every
  sample and refuses the sample if either of them succeeds instead. A receipt that
  hides a failed attempt is a receipt that reports a duration nobody spent.
* No credential appears here or in any log the harness keeps. The first run of
  `start` prints the administrator's password once, so the harness reads it out of
  the raw capture into a shell variable, uses it for the sign-in it is timing,
  clears it, and keeps only the transcript that has passed through
  [`scripts/redact_credentials.sh`](../../scripts/redact_credentials.sh); the raw
  bytes are unlinked at that moment and every file the harness writes is `0600`. A
  case in `apps/platformkit` feeds a synthetic first run through that script and
  refuses a password that survives it.
* `download_bytes` is the archive this database launcher fetches from
  `repo1.maven.org`; it differs by machine (the Linux rows are half the macOS
  rows). This repository pins the PostgreSQL *version*, not a digest, and the
  launcher verifies the archive against a checksum served by that same host, so
  this receipt records the host and makes no integrity promise of its own.
* `artifact` says which bytes were installed: `worktree` is this checkout, installed
  from the tree's own path, and `published` is the proxy's answer to `@main`, the
  command the README prints. A `published` row names in `published_version` what
  `@main` resolved to, and the harness refuses the sample when the proxy answers
  nothing — a claim about a moving branch that does not name what it resolved to is
  a claim about a commit nobody can point at. A `worktree` row names in `revision`
  the commit this checkout stood at and in `worktree` whether the tree was clean
  while the sample ran; a case refuses a revision the tree that quotes it cannot
  reach, because bytes measured in some other checkout are not evidence about these
  bytes. `worktree: dirty` on the rows below is one file — this document and
  `first_use_receipt_test.go`, which were being written while the samples ran. No
  file that `go install` compiles was among them, so the binary each sample measured
  is the one its `revision` names.
* A percentile names its sample count. Five samples is the request; the maximum of
  the samples present is what `p90` means here, and each row below says how many
  samples it has rather than pretending to more. Each summary line is re-derived
  from the receipts of its own `artifact` on its own machine by
  `apps/platformkit/first_use_receipt_test.go`, so a sentence cannot go stale beside
  the lines it summarises and a new kind of row cannot arrive without one.

## linux/amd64

Taken on this repository's own development host, `linux/amd64`, Go `go1.27.3`, a
home with nothing in it, one link to `proxy.golang.org` and `repo1.maven.org`. Two
artifacts are measured on this row and they are not one measurement, so they are
never pooled into one median.

### worktree — this checkout, installed from the tree's own path

RECEIPTS

**worktree — 2 sample(s)** on linux/amd64, Go go1.27.3: median 52987 ms, p90 53986 ms (2 receipts) — under a minute (0.9 min and 0.9 min), and the p90 of two samples is their maximum, which is all a p90 of two can be; two samples is what this round took, and the median of two is their mean. The figure is `fetch_ms + cold_ms + first_action_ms`: the module download, the toolchain download and the compile in a home that has never seen them, the first `start` (PostgreSQL download, extract, `initdb`, migrations, bootstrap, until `/health` answers), and the first authenticated write. The target is median at most 10 minutes and p90 at most 20 minutes; this row is at neither. The warm rerun over the same `--data` is median 3,524 ms and prints no password. Both refusals were answered before anything was spent: the occupied `--addr` in 18 ms and 89 ms, the unwritable `--data` in 54 ms and 149 ms.

{"tool":"scripts/first_use.sh","revision":"ce5362318175a10da7c6c6a5b2d8fa1a17bf418b","published_version":"v1.1.2-proof.20261027.e5fe4025c5ad","go":"go1.27.3","goos":"linux","goarch":"amd64","host":"magneto","artifact":"worktree","worktree":"dirty","download_bytes":14742944,"home_kb":1886508,"cache_kb":933040,"data_kb":50208,"fetch_ms":45317,"cold_ms":8276,"first_action_ms":393,"warm_ms":3197,"cold_plus_first_ms":53986,"task_persisted":true,"rerun_reused":true,"attempts":[{"kind":"occupied_addr","ms":18,"exit":1},{"kind":"unwritable_data","ms":54,"exit":1}]}
{"tool":"scripts/first_use.sh","revision":"ce5362318175a10da7c6c6a5b2d8fa1a17bf418b","published_version":"v1.1.2-proof.20261027.e5fe4025c5ad","go":"go1.27.3","goos":"linux","goarch":"amd64","host":"magneto","artifact":"worktree","worktree":"dirty","download_bytes":14742944,"home_kb":1886860,"cache_kb":933416,"data_kb":50208,"fetch_ms":37894,"cold_ms":13846,"first_action_ms":249,"warm_ms":3851,"cold_plus_first_ms":51989,"task_persisted":true,"rerun_reused":true,"attempts":[{"kind":"occupied_addr","ms":89,"exit":1},{"kind":"unwritable_data","ms":149,"exit":1}]}

### published — what `@main` answered the day this was taken

RECEIPTS

**published — 1 sample(s)** on linux/amd64, Go go1.27.3: median 62341 ms, p90 62341 ms (1 receipt) — one sample, so its median, its p90 and its maximum are one number, and the version it installed is the one `@main` resolved to that day, named in the row beside it.

{"tool":"scripts/first_use.sh","revision":"ce5362318175a10da7c6c6a5b2d8fa1a17bf418b","published_version":"v1.1.2-proof.20261027.e5fe4025c5ad","go":"go1.27.3","goos":"linux","goarch":"amd64","host":"magneto","artifact":"published","worktree":"dirty","download_bytes":14742944,"home_kb":1944552,"cache_kb":936324,"data_kb":50204,"fetch_ms":44793,"cold_ms":17411,"first_action_ms":137,"warm_ms":2524,"cold_plus_first_ms":62341,"task_persisted":true,"rerun_reused":true,"attempts":[{"kind":"occupied_addr","ms":27640,"exit":1},{"kind":"unwritable_data","ms":1829,"exit":1}]}

The number that carries the point on this row is not the median. On the rows above, a
person who had left something answering the port was told so in 18 to 89 ms. Through
`@main` — `v1.1.2-proof.20261027.e5fe4025c5ad`, which does not yet carry this
preflight — the same refusal, on the same machine, with the same harness cost
**27,640 ms**, and the unwritable `--data` **1,829 ms**, because that run downloaded
PostgreSQL, extracted it, ran `initdb`, migrated, bootstrapped a tenant and printed
an administrator's password before it found that something else answered the port.
Those two numbers are what the repair bought, measured through the artifact the
README points at.

## What is not measured

* The other three supported machines. `linux/arm64`, `darwin/amd64` and
  `darwin/arm64` are named by `supportedTargets` and proved by the case that drives
  every supported pair to the database launcher; nothing was installed or started on
  any of them, so no figure here says how long macOS or Arm64 takes, and
  `download_bytes` above is the Linux archive.
* The refusals the harness does not reach. An unsupported machine, a denied
  download, a `postmaster.pid` belonging to a live server, a cluster of another
  PostgreSQL major version, an address needing a privilege this process does not
  have, and the bind won by the preflight and lost after it are answered by cases in
  `apps/platformkit`: that each one refuses, in what words, and before the database
  launcher is reached. None of them is timed here — the only timed refusals are the
  occupied `--addr` and the unwritable `--data`, and they are in every receipt.
* A machine behind a filter, on a slow link, or offline. The network path these rows
  used was this host's, direct to `proxy.golang.org` and `repo1.maven.org`, and
  `download_bytes` is what one host fetched, once.
* Windows, which the command refuses because no release has run it. A `windows/*`
  receipt would be the first, and no row here measures it.
* Two worktree samples and one published sample are two and one. Five is what the
  harness asks for by default; these rows say how many they are, and `@main` moves,
  so the honest way to read the published row is `scripts/first_use.sh --samples 1
  --published` run again rather than this sentence quoted next month.
* What is inside `fetch_ms`, because it is the part a reader will ask about: the
  sample's build cache is its own — a fresh `HOME` makes `GOCACHE` its
  `.cache/go-build`, which `HOME=<fresh directory> go env GOCACHE` answers for any
  directory — and so is the toolchain, since `fetch_ms` opens with
  `go: downloading go1.27.3 (linux/amd64)` because this `go.mod` carries a
  `toolchain` line naming a Go the host's `go` binary is not. A machine already on
  that Go, or past it, pays less than `fetch_ms` says and nothing else in the row
  moves.
