# 10. A limit is a row

Status: accepted, 2026-09-03

## Context

`modules/auth` shipped its lockout as a map in the process: ten failed passwords
for one account in fifteen minutes and that account stops being tried. The
comment on it said what was wrong with it, which is the only reason it was
allowed to ship — *"in this process's memory, and that is the honest limit of
it: with three replicas an attacker gets thirty attempts per window rather than
ten, and a deploy resets the count"*.

Then the deployment stage put two replicas behind one service, and the E6 review
found a client's redeem route measuring 1,054 guesses a second against a code
space small enough to walk. The same gap, in a second module, written the same
way — which is how a missing kernel piece announces itself.

Three things have to be true of the counter that replaces it: every replica sees
one number, a deploy does not reset it, and the thing being counted is refused
before the work it guards is done.

## Decision

**A limit is a row in Postgres, and `kit/limit` is the only thing that writes
it.**

`platformkit_limits(key, window_start, count)`, one row per key per window, and
one statement to record an event:

    INSERT ... VALUES (key, now(), 1)
    ON CONFLICT (key) DO UPDATE SET
      count        = CASE WHEN window_start > now() - window THEN count + 1 ELSE 1 END,
      window_start = CASE WHEN window_start > now() - window THEN window_start ELSE now() END
    RETURNING count

Two replicas raising the same count are serialized by that row's own lock, so
there is no read-then-write for either of them to lose. Around it:

- **A fixed window, not a token bucket.** The limit is stated the way a person
  understands it — ten in a quarter of an hour — and the worst the edge between
  two windows gives an attacker is twice the limit for one instant. A bucket
  costs a second column and a rate nobody can state.
- **The key carries its tenant.** `kit/limit` puts the tenant of the context in
  front of every key, so a limit is per customer without a `tenant_id` column
  and without a policy that reads one. The table is therefore the third
  `platformkit:tenant-scoping-exempt` table in the schema, and its policy is
  `platformkit_is_system()` in both directions: the door is this package, which
  holds the capability, and there is no other.
- **The write is detached and bounded.** A failed login rolls its own
  transaction back — a 401 is a response of 400 or worse, and `kit/httpx` does
  not commit those — so a count written inside it would be a limiter that never
  counts the attempts it exists to count. Every statement runs in its own system
  transaction on a detached context, with a two second budget, because a limiter
  must never be the thing that holds a request open.
- **A limiter that cannot be reached allows the attempt, and says so.** That is
  right for a lockout and would be wrong for a paywall, so the failure is an
  error the caller decides about rather than a silent allowance.
- **`Count` records nothing.** The auth lockout has three answers — allow, delay
  and refuse — so it has to be read before the attempt it is about; a read that
  counted would make every successful sign-in an attempt against the lockout.

## The alternatives we rejected

**Redis, or NATS KV.** Both are better at counters than Postgres is, and both
are a second stateful dependency for a deployment that has exactly one. Redis is
not in this architecture at all; NATS is, but as a transport whose loss is
survivable — the outbox is the durable copy — and a rate limit whose store is
optional is a rate limit that disappears in the incident it exists for. One
database, already there, already backed up, already the thing every replica
agrees about.

**Leaving it per process and buying more replicas' worth of margin.** The
numbers are chosen for what a person legitimately does, so dividing them by the
replica count makes the honest failures fail: ten attempts across three pods is
three per pod, and a person who has forgotten which password this is locks
themselves out on a laptop, a phone and an office network.

## Consequences

- The counters outlive a deploy, and an attacker's window is the window.
- One hourly `DELETE` empties the rows whose window closed a day ago. It runs in
  `modules/auth`'s sweep, because the module that writes the table is the reason
  it exists; the moment a second module adopts `kit/limit`, that purge belongs
  beside the outbox's in `kit/app`.
- A limit now costs a round trip. On a login that is noise next to one argon2id
  hash, which is the shape of every caller so far; a limit on a route that does
  no other work would want a different answer, and this ADR is where the
  argument would be revisited.
- `modules/auth`'s "cluster-wide is a later stage" comments are gone, and the
  memory implementation stayed — as the fake the conformance suite proves the
  interface against, and as what a test uses when it is not testing this.
