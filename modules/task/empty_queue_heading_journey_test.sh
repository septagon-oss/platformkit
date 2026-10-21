#!/usr/bin/env bash
set -euo pipefail

# The empty queue keeps its visible column headings at both widths. A hidden
# phone sort label must not make another heading match the desktop selector.
test_task_queue_keeps_visible_empty_headings() {
  local root
  root="$(git rev-parse --show-toplevel)"
  timeout 1200 "$root/scripts/e2e.sh" empty-list-visible-headings.spec.ts --forbid-only --retries=0 </dev/null
}

test_task_queue_keeps_visible_empty_headings
