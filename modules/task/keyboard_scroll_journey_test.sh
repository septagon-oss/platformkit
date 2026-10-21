#!/usr/bin/env bash
set -euo pipefail

# The generated task queue shares the keyboard-scroll journey with the table.
# Run its existing assertions and application fixture so the subject must still
# exercise real overflow after the queue's responsive layout changes.
test_task_queue_keeps_the_keyboard_scroll_journey() {
  local root
  root="$(git rev-parse --show-toplevel)"
  timeout 1200 "$root/scripts/e2e.sh" scroll-regions.spec.ts </dev/null
}

test_task_queue_keeps_the_keyboard_scroll_journey
