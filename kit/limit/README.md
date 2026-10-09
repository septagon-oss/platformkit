# One counter, and the two ways it can fail

A limit held in a process is three limits when three pods are running and none
after a deploy, so the counter is a row: `platformkit_limits(key, window_start,
count)`, one statement to record an attempt, and that row's own lock to serialise
attempts at the same key. [ADR 0010](../../docs/adr/0010-a-limit-is-a-row.md)
owns that decision. This file owns the one thing it could not say: what a limiter
answers when the statement did not happen.

**Reused**: the row, the statement, the fixed window, the detached system
transaction and its two-second wall, all as ADR 0010 left them; `kit/db`'s
migration of `platformkit_limits` and its `platformkit_is_system()` policy
unchanged; `kit/db/migrate.go`'s way of reading SQLSTATE `55P03` off a driver
error (`contended`) rather than off a message string, which is the precedent for
`waited` below; and `dbtest.Hold` as the way a test slows a store by making
another session hold a real lock. **Added**: the split inside one answer. Nothing
in this package could tell a wait from an outage — `budget` covered the pool,
`BEGIN`, the row-lock wait and `COMMIT` at once, every failure left as
`fmt.Errorf("limit: %w", err)`, and ADR 0010 told every caller to allow the
attempt on an error — so a queue of same-key attempts admitted the flood it
exists to refuse (T-0126 round 83, CI run 52994 job 53535: 76 submissions
admitted at an allowance of 60, none refused). A named queue budget written into
the counter's transaction as `lock_timeout`, an `ErrBusy` for the two methods
that have no `ok` to refuse with, and this README as the one statement of the
failure mode three callers had each been restating. **Made reusable**: the four
rows below, and `dbtest.Hold`. A composer of this package no longer has to decide
what a limiter error means, because the attempt that was never recorded is
answered as a refusal; and any test in this repository can now queue a statement
behind a real holder of a real lock instead of sleeping, which is what makes
`TestABurstAtOneKeyIsAnsweredByTheLimiterRatherThanLeftWaiting` a case rather
than a race.

## The two budgets

| name | what it bounds | why that figure |
| --- | --- | --- |
| `budget` (2 s) | one whole attempt: a connection out of the pool, `BEGIN`, the wait for the row, `COMMIT` | ADR 0010's promise to the caller — this much, and then an answer it can decide about: an error to fail open on where the store replied, a refusal where it spent the budget waiting. A limiter that holds a request longer than that has become the thing it exists to keep off the request, which is why raising it to cover a queue was rejected: the queue is the traffic's own length. |
| `queueBudget` (1 s) | the wait for **this key's row lock** alone, written into the counter's own transaction as `lock_timeout` with `is_local = true` | The server ends that wait and says which world it ended it in (`55P03`), so the classification reads a code rather than a stopwatch. It is measured arriving at the budget and not at the wall. The wall's remaining second pays for the pool, `BEGIN` and `COMMIT`; `TestTheLimitersQueueBudgetFitsInsideItsWall` checks `queueBudget < budget` rather than trusting a comment. |

`lock_timeout` bounds a wait for a **lock**, never a statement's own work, so an
uncontended counter on a loaded runner still runs, counts and answers exactly as
it did. What it also does is stop a parked statement from holding the queue open:
measured across 75 parallel attempts, the longest statement fell from the wall's
2 s to the queue's 1 s.

The consequence is worth stating plainly: more than `queueBudget ÷ one counter
statement` simultaneous attempts at **one key** are refused even while that
window has headroom. That is load-shedding at the one key that is by definition
being flooded. Distinct keys are distinct rows and never queue at all — this is
not a limit on concurrency.

## The failure mode, in one place

Every composer of `kit/limit` shares these four rows. They are the whole of what
`Allow`, `Count` and `Forget` can answer, and nothing outside them is a decision
about a limiter.

| world | how the package learns it | `Allow` returns | `Count` / `Forget` return | what a composer does |
| --- | --- | --- | --- | --- |
| counted | the statement returned | `ok` from the count, `retryAfter` = what is left, no error | the number, what is left, nil | normal path |
| **queued** — the row's lock was not ours within `queueBudget` | SQLSTATE `55P03`, read off the driver error | **`false`, the whole window, no error** | `ErrBusy` | refuse it: `429` with that `Retry-After` |
| **unserved** — the wall expired before anything answered | the attempt's own wall: `context.DeadlineExceeded`, or the bare `driver.ErrBadConn` the pool's driver answers when the wall expires while a connection is still being acquired and the deadline loses its name — the attempt's own **deadline** says which, read rather than awaited: the cancellation `WithTimeout` sends arrives in a goroutine of its own, so on a runner with no free core it lags the wall it announces | **`false`, the whole window, no error** | `ErrBusy` | refuse it, the same way |
| **down** | any other error: no pool on the context (`ErrNoConnection`), a refused connection, a closed database, a denied grant | `false`, 0, the error | the error | **fail open, log once** |

Rows two and three are one answer because they are one fact about the attempt: it
was not recorded, and there is no evidence about the window. Row four is the only
world ADR 0010's fail-open was ever about.

**Row four's action belongs to the composer, and two composers do not fail open.**
The column above says what most of them do; handing the decision over with the
error is the point of row four. `kit/httpx`'s ask-for-access counter (`access.go`'s
`count`) answers an error with `503` — "An outage is never an allowance here" — and
so does the caller of `modules/auth`'s verification-mail counter
(`contracts/limiter.go`'s `VerificationMail`, whose own comment says a failed mail
limiter fails closed). Both read `err` and nothing else, so rows two and three reach
them as `ok false`: neither admits a queued attempt, and neither reads a busy
counter as an outage. Which way a composer chooses is its own and stated by it — a
lockout and an anonymous form fail open because closing them during maintenance
helps nobody; an access request and a mail resend fail closed because an uncounted
one is the thing each of them exists to stop.

**A queued attempt is a refusal and carries no error, and that asymmetry is the
cure.** Most callers in the field — `kit/httpx/public_writes.go`, `modules/auth`'s
lockout, a client's own anonymous-write guard — decide what to do about an error by
allowing the attempt; the two that decide it the other way are named above. If a
queued key answered `(false, window, ErrBusy)` all three would go on admitting the
burst, and
the rule would be one every deployment has to write for itself. `Allow` never
returns `ErrBusy`; `Count` and `Forget` have no `ok` to refuse with, so they answer
it and leave the number unstated rather than inventing one.

**`Retry-After` for rows two and three is the whole window.** The contract is
"what is left of the window", and an attempt that never reached the row read no
window. The whole window is the one figure that cannot understate it. A
non-locking read of the row after the `55P03` was measured and rejected: a second
round trip on a path that is already the slow one, for a value one row-version
stale when it lands, and it would make one header mean two things.

**The fail-open stays, and it stays only in row four.** A store that has vanished
must not stop people signing in or filling in a form; that argument is ADR 0010's
and nothing here weakens it. *Vanished* means it answered — "I cannot serve this", a
refused connection, a denied grant: row four. A store that answers nothing at all, a
dial that goes nowhere or a server that has stopped replying, is row three, because
the wall that ended the wait was this attempt's own; a request that needed that store
fails either way, and what changed is that an attempt is no longer admitted on the
strength of nobody answering. What is refused is refused *with* an answer, and
nothing in this design admits an attempt silently.

## What this does not see

- **The pool is a queue too, and it is not labelled.** 67 of 75 parallel attempts
  in the measured burst were waiting for one of the pool's 16 connections, not for
  the row's lock. That world is refused because a wall that expired is a wall that
  expired, wherever it expired — and it is deliberately *not* split from row four
  by reading `Stats().WaitCount`: that count belongs to the whole pool, so a
  request that merely overlapped somebody else's wait would be read as its own,
  which in the direction that matters turns an outage into a refusal and buys back
  the lockout this rule exists to avoid. A deployment whose pool is saturated
  refuses attempts it could have counted; sizing the pool is the operator's share.
- **A read never queues behind a row.** MVCC answers `Count` from the last
  committed version while a burst queues on the same key, which is why the door
  that reads before it decides — `modules/auth`'s lockout — keeps reading. It also
  means `Count` can only ever meet the wall.
- **A refused attempt is not counted, so a retry may be.** That is intended: the
  row is the truth about who knocked, and an attempt that never reached it cannot
  have been refused by it. It is why `Retry-After` is the whole window.
- **`modules/auth`'s read-then-record shape has a residual.** Its lockout decides
  with `Check` (a `Count`, which never queues) and records with `Failed` (an
  `Allow`, which may). A queued record now arrives as a refusal its `record`
  discards, so a same-account flood can leave the count unraised while the door is
  still open. Making `Failed` report whether the attempt was counted is auth's
  owner's decision, not a change smuggled in beside a kernel fix.
- **Nothing is measured here.** A `pkit.refusal.class` for "the counter was busy"
  would need a fourteenth class in `telemetry.RefusalClass`'s closed set, which is
  the telemetry owner's call. Until then a queued refusal is visible at the door
  that answers it and in one log line, not in a chart.

## Who composes it

Four, all in this repository: `kit/httpx`'s public write limit
(`Options.WriteLimiter`, 60 anonymous submissions a minute per tenant, route and
address), the same limiter spent a second way by `kit/httpx`'s ask-for-access counter
(`access.go`'s `count`, per person and per permission, which answers an error with
`503` where the write limit allows the attempt), `modules/auth`'s lockout and its
verification-mail counter, and `kit/app`'s hourly `Purge` — which is scheduled there
rather than in a module because the table belongs to whoever holds a limiter and to
nothing else.

`Postgres` is given a pool, not a request context, and the difference matters:
the public write limit runs *ahead* of the middleware that puts a connection on a
request, so a composer that hands it `httpx.ConnFrom` gets `ErrNoConnection` on
every public write and — row four — admits all of them. `kit/app` composes
`limit.Postgres(a.held.read)`, the pool held open-handed from `New`; do the same.
