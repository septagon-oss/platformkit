# First use, measured

This is the receipt for [the README's one command](../../README.md#try-it-locally).
It exists because an installation promise that has no numbers behind it is a
description, and because the parts of a first run that are not this program's
fault — the module download, the toolchain, the PostgreSQL download, every attempt
that failed — are the parts a stranger actually spends.

`scripts/first_use.sh` takes the measurements and writes one JSON object per line,
into the file named by `--out` or `FIRST_USE_OUT`:

```sh
scripts/first_use.sh --samples 5 --out /tmp/first-use.jsonl
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

The number the acceptance target is written against is
`fetch_ms + cold_ms + first_action_ms` — the whole public action from an empty home
to a persisted task, including the failed attempts it costs.

## The rules a receipt obeys

* A receipt is written only by the path that makes it true. A sample that refused,
  was interrupted, or could not read its own timings writes nothing, so no receipt
  in this file describes a run that did not happen.
* `attempts` is never empty, because the harness runs two failing attempts on every
  sample and refuses the sample if either of them succeeds instead. A receipt that
  hides a failed attempt is a receipt that reports a duration nobody spent.
* No credential appears here or in any log the harness keeps. The first run of
  `start` prints the administrator's password once, so every captured transcript
  goes through [`scripts/redact_credentials.sh`](../../scripts/redact_credentials.sh)
  before it is kept; a case in `apps/platformkit` feeds a synthetic first run
  through that script and refuses a password that survives it.
* `download_bytes` is the archive this database launcher fetches from
  `repo1.maven.org`; it differs by machine (the Linux rows are half the macOS
  rows). This repository pins the PostgreSQL *version*, not a digest, and the
  launcher verifies the archive against a checksum served by that same host, so
  this receipt records the host and makes no integrity promise of its own.
* `published_version` is what `@main` resolved to at the time of the run. A claim
  about a moving branch that does not name what it resolved is a claim about a
  commit nobody can point at.
* A percentile names its sample count. Five samples is the request; the maximum of
  five samples is what `p90` means here, and each row below says how many samples
  it has rather than pretending to more.

## linux/amd64

Taken on this repository's own development host, `linux/amd64`, Go `go1.27.3`, a
home with nothing in it. The `worktree` rows install this checkout, so they measure
the first-use path as it stands in this tree; the published row installs through
the proxy at `@main`, which is the command the README prints.

RECEIPTS
**3 sample(s)** on linux/amd64, Go `go1.27.3`: median 40586 ms, p90 47224 ms (3 receipts) — under a minute (0.7 min and 0.8 min), and the p90 of three samples is their maximum, which is all a p90 of three can be. The figure is `fetch_ms + cold_ms + first_action_ms`: the module download and compile in a home that has never seen them, the first `start` (PostgreSQL download, extract, `initdb`, migrations, bootstrap, until `/health` answers), and the first authenticated write — with the two failed attempts of each sample timed into `attempts` beside it, because they are time the person spent. The target is median at most 10 minutes and p90 at most 20 minutes; this row is at neither. The warm rerun over the same `--data` is median 2,381 ms and prints no password. Both refusals are answered in 19–24 ms — the occupied `--addr` used to be found after 29,034 ms, one PostgreSQL download, one tenant and one printed password. **What this row does not include:** `GOCACHE` was the host's, so the compile inside `fetch_ms` reused objects this machine had already built; a machine whose build cache is empty pays more, and the row does not say how much. `artifact` says which bytes were measured — `worktree` is this checkout installed into the sample's home, and `worktree: dirty` records that the harness's own file was uncommitted while it ran.

{"tool":"scripts/first_use.sh","revision":"9bdc00bd65790328895ee37411d7292023dfc405","published_version":"","go":"go1.27.3","goos":"linux","goarch":"amd64","host":"magneto","artifact":"worktree","worktree":"dirty","download_bytes":14742944,"home_kb":1897008,"cache_kb":933452,"data_kb":50208,"fetch_ms":34126,"cold_ms":12908,"first_action_ms":190,"warm_ms":2381,"cold_plus_first_ms":47224,"task_persisted":true,"rerun_reused":true,"attempts":[{"kind":"occupied_addr","ms":21,"exit":1},{"kind":"unwritable_data","ms":23,"exit":1}]}
{"tool":"scripts/first_use.sh","revision":"9bdc00bd65790328895ee37411d7292023dfc405","published_version":"","go":"go1.27.3","goos":"linux","goarch":"amd64","host":"magneto","artifact":"worktree","worktree":"dirty","download_bytes":14742944,"home_kb":1897224,"cache_kb":933680,"data_kb":50208,"fetch_ms":34053,"cold_ms":6331,"first_action_ms":202,"warm_ms":3043,"cold_plus_first_ms":40586,"task_persisted":true,"rerun_reused":true,"attempts":[{"kind":"occupied_addr","ms":19,"exit":1},{"kind":"unwritable_data","ms":19,"exit":1}]}
{"tool":"scripts/first_use.sh","revision":"9bdc00bd65790328895ee37411d7292023dfc405","published_version":"","go":"go1.27.3","goos":"linux","goarch":"amd64","host":"magneto","artifact":"worktree","worktree":"dirty","download_bytes":14742944,"home_kb":1897324,"cache_kb":933788,"data_kb":50208,"fetch_ms":33454,"cold_ms":6879,"first_action_ms":189,"warm_ms":2316,"cold_plus_first_ms":40522,"task_persisted":true,"rerun_reused":true,"attempts":[{"kind":"occupied_addr","ms":23,"exit":1},{"kind":"unwritable_data","ms":24,"exit":1}]}

## What is not measured