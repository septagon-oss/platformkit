# Floci in place of SeaweedFS in the test stack, measured

This spike asks whether the object store this repository's test stack starts could be Floci rather than
SeaweedFS, and decision 0083 says the answer comes from the suite the kernel already ships and the
numbers the host already has. It changes no default: the candidate arrives as one service,
`s3-floci`, behind a new `floci` profile, so `make up` starts the same five containers it started before
this file existed and the switch below is proposed rather than made.

Measured at commit `3751ffa`, 2026-10-17 UTC, on the host this program's loop runs on — `x86_64`, Docker
29.9.1, Docker Compose 5.6.0, `MemTotal: 65463624 kB` — with the load average moving between 10.95 and
28.04 and the running-container count between 159 and 164 while the figures were taken. That is the
premise: a task's stack is one of a hundred and sixty containers on this machine. Both stores were
booted twice, because neither one's footprint turned out to be stable across boots.

## What was measured

The two backends as pinned in `compose.yaml`:

| | the store the stack runs today | the candidate |
|---|---|---|
| reference | `chrislusf/seaweedfs@sha256:4e61d15fd35994cb1e43e1e553dff106794841fd9a99ade2fc8c8bfce4d7872d` (service `s3`) | `floci/floci:2.3.0@sha256:e0b292df4dc9559b7b1bf4f51bdda4bb8e269276236438cd03cae661260ea8a9` (service `s3-floci`, `profiles: ["floci"]`) |
| image size, as the daemon reports it | 723,693,732 bytes | 345,463,540 bytes — 2.09× smaller |
| S3 answers on | container port 8333 | container port 4566 |
| readiness probe | the compose service's own `curl`, 5 s interval — the image ships none | the image's own `/usr/local/bin/healthcheck.sh`: 5 s interval, 10 s start period, 50 ms start interval, so the compose service declares none and could not declare `s3`'s: the ubi9-micro base carries no `curl` |
| licence | Apache-2.0 | MIT, read from `LICENSE` (1,083 bytes) at the source repository `github.com/floci-io/floci`; the `ubi9-micro-container` base layer carries Red Hat's own EULA label beside Floci's MIT label |
| credentials | the stack's `AWS_ACCESS_KEY_ID` / `AWS_SECRET_ACCESS_KEY`, named in the service | none named — Floci accepts any non-empty SigV4 credential, so naming the stack's would name a user that exists nowhere; the suite sent `pkittest`/`pkittestsecret` and was answered |

    $ timeout 120 docker buildx imagetools inspect floci/floci:2.3.0
    Name:      docker.io/floci/floci:2.3.0
    Digest:    sha256:e0b292df4dc9559b7b1bf4f51bdda4bb8e269276236438cd03cae661260ea8a9
      Manifests: …@sha256:09b72c45f333019adecb302e79c6b746e290135f528016c8514618bc7ab02344  linux/amd64
                 …@sha256:2b60d1b3843084a366cc05d86f089f8e54271b4b62094ea6706831bbea277a00  linux/arm64
    $ timeout 90 curl -sL https://raw.githubusercontent.com/floci-io/floci/main/LICENSE | head -2
    MIT License
    Copyright (c) 2025 Floci and its contributors.

That proof is quoted because `floci/floci:latest` answers the *same* index digest as `floci/floci:2.3.0`
today, and because an image's Docker Hub namespace is not where its licence lives. The service pins tag
and digest together, so a moving `latest` cannot slip under a measurement or under a stack.

The three claims the candidate's own README makes, and this file tests or respects, come from the same
source:

    $ timeout 90 curl -sL https://raw.githubusercontent.com/floci-io/floci/main/README.md \
        | grep -n "Idle memory\|Docker image size\|Credentials can be any non-empty\|Entirely in RAM\|FLOCI_STORAGE_MODE\|first request pulls the console image"
    84:All AWS services are available at `http://localhost:4566`. … Credentials can be any non-empty
       values unless you explicitly enable stricter service-specific auth checks.
    139:Nothing runs at boot. The first request pulls the console image, starts it as a sidecar container
        on Floci's Docker network, hands it Floci's own reachable address …
    243:| Idle memory | ~13 MiB | ~143 MiB |
    244:| Docker image size | ~90 MB | ~1.0 GB |
    458:Floci can trade speed for durability depending on the workflow. Configure the default mode with
        `FLOCI_STORAGE_MODE`, or override storage per service.
    462:| `memory` | Entirely in RAM. Data is lost when the container stops. | CI and ephemeral tests | None |
    806:| `PERSISTENCE=1` | `FLOCI_STORAGE_MODE=persistent` |
    862:| `FLOCI_STORAGE_MODE` | `memory` | Storage mode: `memory`, `persistent`, `hybrid`, or `wal` |

The `~13 MiB` on line 243 is the premise this spike was asked to test; the storage-mode default on line
862 is why the service mounts no volume; the credentials rule on line 84 is why it names none; and line 139
is the sidecar risk named in the verdict.

Three arms, both backends, same commit, same credentials, same host, with nothing else started by this
task except a Postgres from the same compose file (case 14 migrates a schema). The context line, so a reader
on a quieter machine knows what these figures were taken against:

    $ cat /proc/loadavg; docker ps -q | wc -l; uname -m; docker version --format '{{.Server.Version}}'; docker compose version
    18.81 19.63 18.18 15/13151 3740030
    159
    x86_64
    29.9.1
    Docker Compose version 5.6.0

The three arms, on both backends:

1. **Image size** for the pinned reference, in bytes, from `docker image inspect --format '{{.Size}}'`.
   Floci's README claims `~90 MB` for its image; that is a different measure of a different thing, and the
   daemon's byte count is what both backends are held to here.
2. **Start and idle memory.** Both stores up in one throwaway project (`-p pkit999-t0281-impl`), each on
   a loopback port allocated by `scripts/free_port.sh`'s `bind_free_port`, with no traffic on either, read at
   container ages 30 s, 2 min, 5 min and 8 min, and started a second time with `--force-recreate`. An age
   is that sample's own printed time minus that container's `State.StartedAt`. Health facts are read in the
   same command that starts the container, because Docker keeps only the last five probe entries and a
   later read shows the last five minutes of probes rather than the first.
3. **Conformance** against one endpoint at a time, three runs each, so neither store is measured with the
   other's traffic on it.

Every memory sample reads two things, because they are not the same thing:

    docker stats --no-stream --format '{{.MemUsage}} {{.MemPerc}} {{.CPUPerc}}' "$cid"
    docker exec "$cid" cat /sys/fs/cgroup/memory.current /sys/fs/cgroup/memory.stat

The first is the container as Docker reports it, and on cgroup v2 that includes the page cache charged to
the container. The second is the kernel's own split of the same container into `anon` — what the process
holds — and `file`, which is reclaimable and includes a memory-mapped binary. The verdict turns on that
split, so both appear in the table.

The suite is the one this repository already ships: `filetest.RunStorage`
(`modules/file/contracts/filetest/storage.go`) driven through `modules/file/s3_test.go`, the three live
cases beside it, and the upload route's case in `modules/file/upload_store_test.go`. Its fixture takes the
endpoint, keys and bucket prefix from the environment and fails rather than skipping when nothing answers,
so pointing it at another store is an environment change and no code change:

    PLATFORMKIT_TEST_S3_ACCESS_KEY=pkittest PLATFORMKIT_TEST_S3_SECRET_KEY=pkittestsecret \
    PLATFORMKIT_TEST_S3_BUCKET_PREFIX=platformkit-spike \
    PLATFORMKIT_TEST_S3_ENDPOINT=localhost:<that store's own allocated port> \
    go test ./modules/file/ -count=1 -v -run \
    'TestTheS3AdapterKeepsTheStorageContract|TestAnObjectOneTenantWroteOpensNothingForAnother|TestAGrantIsAPresignedReadOntoOneTenantsObject|TestAnS3CallWithNoScopeIsRefused|TestAnUploadArrivesInTheObjectStoreTheCompositionChose'

Two things were deliberately not measured. The `Reconciler` listing arms are not implemented by this
adapter, so they run nowhere (below). And nothing was asked of Floci's other AWS services: it serves SQS,
SNS, DynamoDB and more in the same process, this repository uses none of them, and every memory figure
here is therefore a figure for a whole emulator, not for its S3.

## Conformance

`ok` with a timing is that backend's own `-v` line for that case.

| # | case, as the suite prints it | what it asks of the store | SeaweedFS | Floci |
|---|---|---|---|---|
| 1 | a scope names one tenant's prefix and no other tenant's bytes | another tenant's key answers not-found; deleting what you do not hold is an idempotent success, not a refusal | `ok` 0.22s | `ok` 0.04s |
| 2 | a scope with no tenant is refused and writes nothing | the refusal is the adapter's; no request reaches the store | `ok` 0.00s | `ok` 0.00s |
| 3 | a key this module could not mint is refused before any byte is read | `ErrInvalidKey` for a traversal, a non-UUID, an empty key, an upper-cased UUID | `ok` 0.00s | `ok` 0.00s |
| 4 | non-seekable streams and empty blobs | a 0-byte and a 17,408-byte body, each handed over as a bare `io.Reader`, round-trip byte for byte | `ok` 0.01s | `ok` 0.01s |
| 5 | a body that declares no length arrives, and collides | an undeclared write lands, then collides in both directions against a declared one | `ok` 0.02s | `ok` 0.03s |
| 6 | collisions preserve the original bytes | a conditional create onto bytes that exist is refused, and the first bytes survive | `ok` 0.01s | `ok` 0.01s |
| 7 | concurrent creation has one complete winner | eight simultaneous creates of one key: exactly one reports success, and the object holds that writer's whole stream | `ok` 4.01s | `ok` 0.01s |
| 8 | missing blobs and repeated deletion | not-found on read; delete of a missing key succeeds, and succeeds again | `ok` 0.01s | `ok` 0.01s |
| 9 | reader failure cannot become upload success | a caller stream that breaks mid-body fails the `Put`, and the key is then deletable | `ok` 0.08s | `ok` 0.19s |
| 10 | independent stores, and a listing that honours its cutoff and its scopes | two prefixes in one bucket, each seeing only its own bytes | `ok` 0.01s | `ok` 0.00s |
| 11 | `TestAnObjectOneTenantWroteOpensNothingForAnother` | across tenants: not-found, no-op delete, the same key free in the second prefix, the first tenant's bytes intact, both names present in the bucket's own listing | `ok` 0.26s | `ok` 0.02s |
| 12 | `TestAGrantIsAPresignedReadOntoOneTenantsObject` | a presigned URL answers 200 with the object's bytes and the `Content-Type` the object carries; the same file signed under another tenant's scope is a different URL and does not answer 200 | `ok` 0.28s | `ok` 0.03s |
| 13 | `TestAnS3CallWithNoScopeIsRefused` | every call with an empty scope and four bad keys refuse, and the bucket is left **empty** | `ok` 0.01s | `ok` 0.02s |
| 14 | `TestAnUploadArrivesInTheObjectStoreTheCompositionChose` | `POST /files`, which can honestly declare no length, arrives as exactly one object under the tenant's prefix, holding the posted bytes | `ok` 0.43s | `ok` 0.17s |
| — | the `Reconciler` listing arms inside case 10 | prefix listing, cutoff, scope filtering | **not run**: `file.S3` is not a `contracts.Reconciler`, so the harness skips them | **not run**: the same adapter, the same reason |

Wall time of the whole `go test` command, three runs per backend. The first run of each pair warms the
build cache, so its number includes compilation and is labelled rather than averaged in:

| backend | run 1 (with `-v`) | run 2 | run 3 | median | `go test`'s own package line, same three runs |
|---|---|---|---|---|---|
| SeaweedFS | 6745 ms | 2765 ms | 5141 ms | 5141 ms | 5.376s, 1.549s, 3.734s |
| Floci | 2288 ms | 1677 ms | 1782 ms | 1782 ms | 0.620s, 0.431s, 0.648s |

The spread *within* one backend — 2765 ms to 6745 ms for the same five functions against the same
container — is wider than the gap between the two backends' fastest runs, so these six numbers say that
the suite is not a benchmark on a host carrying a hundred and sixty other containers, and nothing more.
The two clocks are named apart because they differ: the package line is Go's, the wall time adds process
start, bucket creation and bucket teardown.

One failure belongs in this section, because it is why the arm can be trusted. The candidate's first three
runs came back `dial tcp [::1]:45145: connect: connection refused` in all five functions: the command that
started `s3-floci` had omitted `PLATFORMKIT_S3_FLOCI_PORT`, so Compose published the service on its default
8334 while the suite dialled the allocated port. The figures above are the corrected run, whose start line
prints `published=0.0.0.0:45145` beside its healthy probe. The fixture refuses to skip a store it cannot
reach, which is what turned that into one readable line rather than a green run against somebody else's
container.

**What fifteen green assertions cannot say.** `modules/file/internal/s3.go` splits a declared body above
16 MiB into a multipart upload, and the adapter's claim-then-fill path exists because that is the request
at which `If-None-Match: *` stops being answered on the store this stack runs today. The suite's largest
body is 17,408 bytes, so **no case above exercises a multipart create — not on the candidate, and not on
the store CI uses either, before or after this task.** What the suite does establish about conditionals is
not nothing: cases 5, 6 and 7 cannot pass against a store that answers every conditional write with
success, so Floci honours `If-None-Match: *` on a single-part create. Closing the multipart arm needs a
body over 16 MiB inside `filetest.RunStorage`, which changes the harness every storage implementation
shares: a kernel decision, not a spike's.

## Memory and start time

### Idle memory

Two boots of each store (`--force-recreate` between them), no traffic while sampled. Each cell carries
`docker stats` in the units the daemon prints and, where the cgroup read succeeded, `anon`: the working
set. Ages are seconds since that container's own `StartedAt`, and boot B is boot A recreated — the third
bullet below turns on what that changed.

| boot | container | age 30 s | age 2 min | age 5 min | age 8 min |
|---|---|---|---|---|---|
| A | SeaweedFS | 68 MiB @36.5 | 71.61 MiB @126.5 | 72.07 MiB, anon 73.5 MB @312.9 | — |
| A | Floci | 24.75 MiB @30.0 | 100.9 MiB @120.0 | 105.6 MiB (its `memory.current` 147 MB) @306.4; read again at 392 s: anon 35.4 MB, file 100.4 MB | — |
| B | SeaweedFS | 164.1 MiB, anon 169.5 MB @53.0 | 165 MiB, anon 174.3 MB @143.1 | 175.3 MiB, anon 170.6 MB @323.1 | 165.2 MiB, anon 170.7 MB @503.1 |
| B | Floci | 26.35 MiB, anon 30.5 MB, file 3.0 MB @33.2 | 31.25 MiB, anon 34.5 MB @123.3 | 35.46 MiB, anon 40.1 MB @303.2 | 38.27 MiB, anon 43.0 MB @483.2 |

Serving the suite moves both, and moves them apart. The two boots above were recreated before the suite
ran, so the figures below come from the containers that actually served it, each age named beside the run
it follows.

| container, after serving | docker stats | `anon` | when |
|---|---|---|---|
| SeaweedFS, its three suite runs | 225.3 MiB | 237 MB | 30 s after the third run, container age 521 s |
| SeaweedFS, restarted and served one run | 180.5 MiB, then 180.6 MiB | 186.8 MB, then 186.8 MB | 121 s and 241 s after that run |
| Floci, its three suite runs | 86.16 MiB | 78.3 MB | 7 s after the third run, container age 7 s (the container was recreated for this run) |
| Floci, the same container | 86.2 MiB, then 86.25 MiB | 78.3 MB, then 75.3 MB | 241 s and 361 s of container age, about 234 s and 354 s after the third run |

* **The brief's premise does not survive measurement.** `~13 MiB` was not observed at any age on this
  host. The candidate held 30.5 MB anonymous at 30 s, 43.0 MB at 8 min and 75–78 MB after serving the
  suite — its own background schedulers are visible in its log, and the curve had not flattened at 8 min.
  The *direction* of the saving is real; the figure to quote is a series, not 13.
* **On anonymous memory the candidate is lower at every age in both boots:** 30.5 MB against 73.5 MB
  (boot A) and 169.5 MB (boot B) at half a minute, 43.0 MB against 170.7 MB at 8 min, 75–78 MB against
  187–237 MB after traffic.
* **The daemon's figure alone reverses the answer.** In boot A the emulator's container carried 100.4 MB
  of `file` pages — the sample names the class of page and not its source, but the shape is the one a
  process mapping and reading a 345 MB binary leaves — and reported 105.6 MiB, *more* than SeaweedFS's
  72.07 MiB at the same age. In boot B the same image on the same host carried 3.0 MB of them and reported
  35.46 MiB against SeaweedFS's 175.3 MiB. Page cache is reclaimed under pressure; anonymous memory is not.
  A comparison quoting only `docker stats` gets this backwards depending on what the host read first — and
  on a host carrying other tasks' stacks that is an accident waiting to happen rather than a hypothetical.
* **The incumbent's idle footprint is not a property of its image either.** The same pinned digest held
  73.5 MB anonymous in one boot and 169–174 MB in another, at the same ages, with no traffic on either and
  nothing in the stack changed between them. Why was not chased: a spike comparing stores has to treat the
  store's own runtime as given, and a heap that a Go runtime sizes to its machine is as much a fact about
  the incumbent as its digest is. One boot of either store would have produced a ratio out of two numbers
  and been wrong half the time.

### Start time

Two clocks, named apart, because they measure different things at different resolutions.

| boot | container | `docker compose up -d --wait` returned | `StartedAt` → first passing probe | the ruler that bounds it |
|---|---|---|---|---|
| A | SeaweedFS | 6466 ms | 5.28 s | its probe's 5 s interval — readiness lies somewhere inside that band |
| A | Floci | 11431 ms | 9.88 s, after four probes refused | 50 ms start-interval inside a 10 s start period: ±90 ms |
| B | SeaweedFS | 31622 ms | 20.33 s, after three probes exited 7 | its probe's 5 s interval |
| B | Floci | 1592 ms | 0.20 s, first probe passed | 50 ms start-interval inside a 10 s start period |

The emulator took 9.88 s to answer 4566 on its first boot in this task and 0.20 s on a later one — same
image, same digest, same host, with 160 other containers between the two, which is as much as this spike
claims about why. The incumbent's two boots were 5.28 s and 20.33 s. So the one start-time claim that holds
without depending on the host's page cache or on a probe's resolution is the one that matters for the stack:
**the candidate reached `healthy` under `make up`'s own `--wait` with no compose-side healthcheck at all**,
in both boots, because the image declares its own. That is what the profiled service in `compose.yaml` tests
every time anybody starts it, and it is what a service copied from `s3:` would have got wrong — the probe
above it is a `curl`, and this image carries none.

### The stack's shape, and its teardown

    $ docker compose config --services
    mailpit nats postgres s3 valkey
    $ docker compose --profile floci config --services
    mailpit nats postgres s3 s3-floci valkey

`make up`'s list is what it was. `make down` now names both profiles, and that line is measured rather
than tidy: a `down` that names one profile does not stop the other's container, prints
`Network … Resource is still in use`, and exits 0 — one leaked container per stack that ran the spike,
which is the cost this task exists to reduce rather than to add. The teardown after these measurements
left both `docker ps -a --filter name=pkit999-t0281-impl` and
`docker network ls --filter name=pkit999-t0281-impl` empty.

## Verdict

**The conformance arm passes; the memory premise does not; the switch is therefore proposed with one
figure corrected, and that corrected figure is what the measurement was for.**

*Conformance.* Every case that runs ran green against Floci at the pinned digest, three runs, from the
same commit as the SeaweedFS run, with the two `Reconciler` arms named as not-run on both sides instead of
counted. The candidate honoured `If-None-Match: *` on a single-part create and gave exactly one winner to
eight concurrent creates. What stays unproven — the multipart conditional arm — stays unproven on the
incumbent too, which makes it a hole in the harness rather than a difference between stores.

*Memory.* Lower than the incumbent at every measured age in both boots **on anonymous memory**, and *not*
lower on the figure the brief quotes, which is the daemon's and includes page cache: in boot A the
candidate's container reported more than the store it would replace. The numbers to carry forward are 30 MB
anonymous at half a minute, 43 MB at eight minutes and 75–78 MB after traffic, against 73–175 MB idle and
187–237 MB after traffic today — two to four times lighter per container, not the sixfold the brief's
`~13 MiB` implied.

*Start.* Reaches `healthy` under `make up`'s own `--wait` with no compose-side healthcheck, in both boots.
The first boot measured here took ~10 s to answer 4566 and a later one 0.2 s, so a stack that has to start
the candidate from a cold page cache waits longer for it than it does today; on a warm one the difference is
inside a health-probe interval.

*Checks.* The parts `make check` is made of ran green with the service present: the fourteen goals that
need no shared slot (`vet`, `fmt-check`, the budgets, the package, GUC, UI-layer, bundle, version, run-owner,
pillar and step-library gates, the test inventory and the pseudo-locale gate) in one call at rc=0; then
`go tool gotestsum --packages='./...'` over the whole tree, 104 packages and no failure among them; then the
recipe's guard scripts, until the host took that run down partway through. Its last two guards were then run
by hand and passed. The whole command's own exit status is therefore the gate's to record and not this
document's. The service sits outside `make up`, so what that run exercises is that nothing in the tree
reaches past the profile into it — the property the stack's own guards on `compose.yaml` and the `Makefile`
are asked about.

Root is asked to decide the switch on the anonymous-memory figures rather than on `docker stats`, and to
take it in the same change as the four edits it costs, none of which this task makes:

1. `modules/file/store_gate_test.go`'s image pattern (`seaweedfs|minio/minio|quay\.io/minio`) is this
   repository's definition of "an image that speaks S3" for the workflow gate; a CI swap that leaves it
   alone leaves behind a definition that no longer names the store CI runs.
2. `scripts/ci_container_leak_test.sh` holds the SeaweedFS digest as the literal text it mutates; a CI
   image that moves without it moves the guard's needle out from under it.
3. `modules/file/README.md` states that the stack "starts SeaweedFS (`make up`, published on 8333)", and
   the port is where the candidate would differ.
4. `ARCHITECTURE.md`'s "Object storage: modules/file over S3" paragraph cites the pin by compose line
   number (`compose.yaml:122`) while the image line sits one below that — a citation stale by one line
   before this task, and one a swap rewrites anyway, since it names SeaweedFS in the sentence around it.

Two consequences a switch takes on, named so nobody discovers them afterwards. The memory figure is for the
whole emulator, which serves SQS, SNS, DynamoDB and the rest in one process none of which this repository
uses. And Floci's web console, at `/_floci/ui`, pulls a console image and starts a sidecar container on
Floci's own Docker network the first time it is requested: nothing in this suite asked for it, nothing here
ever asked for it, and a memory figure taken after somebody opens that page measures a different thing.

## Reused, added, made reusable

**Reused** — `filetest.RunStorage`, the `s3Store` fixture and the four live store cases
(`modules/file/contracts/filetest/storage.go`, `modules/file/s3_test.go`,
`modules/file/upload_store_test.go`) carried every case above with no change to any of them;
`compose.yaml`'s existing `profiles:` mechanism, already used for the telemetry collector, carried the
candidate service; `scripts/free_port.sh`'s `bind_free_port` allocated every port; `dbtest.Schema` and the
file module's own migrations carried the upload case; the `Makefile`'s one teardown line carried the second
profile.

**Added** — the `s3-floci` service and its `floci` profile, because nothing in the tree named Floci and so
no existing mechanism could start the candidate at all; `--profile floci` on `make down`, because a teardown
that names one profile leaks the other's container and still exits 0; and this document, because the
repository records no measurement anywhere — `make up` waits and writes nothing down — so these figures had
no file to live in and no format to be quoted in.

**Made reusable** — the measurement protocol, as a command sequence with its ages fixed and its two clocks
named: `docker stats` read beside the cgroup's `anon`/`file` split, "time to healthy" read out of the
daemon's health log in the same command that starts the container, one store under traffic at a time, and
every store booted twice because neither one's footprint is stable across boots. The distinction it turns
on — a container's page cache is not its working set — applies to every memory claim this program makes
about its test stack, not only to this comparison.
