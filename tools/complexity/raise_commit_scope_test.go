package main

import "testing"

// A ceiling raise stands in its own commit: an approved raise that also
// carries other files is refused, one that carries only the budget passes.
func TestApprovedRaiseMustTouchOnlyTheBudget(t *testing.T) {
	r, base := fixture(t, 1)
	putBudget(t, r, 2)
	writeFile(t, r.root, "kit/httpx/extra.go", "package httpx\n")
	commit(t, r, approved)
	historyResult(t, r, base, 1)

	r, base = fixture(t, 1)
	putBudget(t, r, 2)
	commit(t, r, approved)
	historyResult(t, r, base, 0)
}
