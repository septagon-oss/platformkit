---
repo: platformkit
wave: 1
priority: high
requires: [task-T-0331]
worker: pi
gates:
  specify:
    - "go build ./... && go vet ./..."
  implement:
    - "make check"
---
# T-0337 — The kernel counts what each resource holds, for the person asking

## Why (root and Codex, 2026-10-09; the owner: "the things we need to start improving in this default app is the UI
and UX, how we show users the little details really matter here … the user experience needs to be great right from
the start")

Decision 0085 (`/home/jplr/gitrepos/pkit/ops/platformkit-record/architecture/999-apps/decisions/0085-the-phone-feels-finished-from-the-first-minute.md`) records what a person sees today, with emulator screenshots of phone `9a62095`
(`0085-evidence/*.png` beside it), the principles, the exact copy table, the catalogue contract and the order of work.
Read it first; this task is one step of it. Codex reviewed these briefs against the code; the file and line
references below are from that review.

**Wave two (the owner, 2026-10-09: "I would like to see images and cool visual tricks", then "great lets push this
forward").** The owner reviewed ten image and motion techniques with app references on a private page
(claude.ai/artifact/XU64EgDrBfpM2VvFbBkx9L) and asked for all of them. 0085's principles still hold: every effect
respects reduced motion, is driven by data the catalogue declares (never guessed), and has a still state that reads
on its own. Animation uses `react-native-reanimated` (installed, unused today).

> **Decision 0091 D3, re-ordered by root 2026-10-12:** this task ships its hint now, the way T-0325's hints ship; the
> presentation policy T-0403 builds afterwards carries it into `resolved` (T-0403 requires this task, so the policy
> is complete from its first version). Add no `resolved` key here.

## Decided (do not ask root)

1. `GET /api/v1/app/summary` returns, for each catalogue entry the person may read, an authorised total and an
   optional declared highlight (entry presentation `highlight: {label, filter}`, e.g. tasks "Due today" with a
   filter on `dueAt`), plus `asOf`. Absence means unavailable, never zero.
2. Totals respect tenancy and permissions exactly as the list does; one query per entry, bounded; cached briefly per
   tenant and person.
3. Optional per-entry daily counts for the last 14 days (`trend`) when the entry declares `trendField` (a timestamp),
   for a sparkline.
4. Adopt on tasks (total open, highlight "Due today", trend on `createdAt`).

## Acceptance

- `make check` passes; tests: permission-filtered totals, cross-tenant isolation, unavailable vs zero, the tasks
  highlight.

## Specify

The response schema, the query per entry, and the cache rule.

## Implement

Make the change; commit with `Verified:` and `Not verified:`.
