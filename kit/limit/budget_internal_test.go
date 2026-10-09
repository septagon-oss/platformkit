package limit

// budget_internal_test.go checks the two durations as a property of the package
// rather than as a comment beside them. They are the numbers this cure is made
// of, and the relation between them is the one that has to hold: a queue budget
// longer than the wall would never answer, and a wall with no queue budget inside
// it is the limiter that admitted a burst.

import "testing"

// TestTheLimitersQueueBudgetFitsInsideItsWall is the invariant of the two
// budgets: waiting for this key's row is a part of what one attempt may take,
// and not a second budget beside it. The wall's remainder pays for the rest of
// what the attempt waits for — a connection out of the pool, BEGIN, COMMIT — so
// a queueBudget of the whole budget would leave those waits unfunded and a
// queueBudget of nothing would refuse an attempt that never had a chance.
func TestTheLimitersQueueBudgetFitsInsideItsWall(t *testing.T) {
	if queueBudget <= 0 {
		t.Errorf("queueBudget is %s, want a wait a queued attempt can use", queueBudget)
	}
	if queueBudget >= budget {
		t.Errorf("queueBudget %s is not inside budget %s: a wait longer than the wall is never answered, "+
			"and the wall's remainder is what pays for the pool, BEGIN and COMMIT", queueBudget, budget)
	}
}
