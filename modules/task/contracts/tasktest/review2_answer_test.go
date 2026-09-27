package tasktest_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

// Review 2, finding 1 of review 1, second instance. Review 1 proved the lost
// retry assertion against contenttest's fake only and left this one read from
// the code ("the same hole is open at tasktest's 'assign is idempotent for the
// same assignee'", and its own Unverified list says so). The cure claims the
// assertion is back "at every mutating operation of all three ports", so the
// case the old hand-written one made — got.Status and *got.AssigneeID on the
// second Assign — must now refuse a service that stores the right task and
// answers a different one.
//
// This pins that claim for tasktest. It passes when the retry compares the two
// answers and fails if the comparison is ever dropped or rendered vacuous
// (an `answer` that stops carrying the status and the assignee is the same
// regression in a different place).
func TestTheRetryReadsWhatAssignAnswered(t *testing.T) {
	// Reachability through the fixed behaviour: the fake the package ships
	// passes its own suite, so a failure below belongs to the mutant.
	if ok := runTaskSuiteQuietly(t, nil); !ok {
		t.Fatalf("the conformance suite does not pass the fake it ships with; nothing below means anything")
	}
	if ok := runTaskSuiteQuietly(t, func(s contracts.Service) contracts.Service {
		return &answersAnotherTask{Service: s, seen: map[uuid.UUID]bool{}}
	}); ok {
		t.Errorf("the conformance suite passed a service whose second Assign stores the assignee it was given and " +
			"answers an unassigned task; \"assign is idempotent for the same assignee\" asserted got.Status and " +
			"*got.AssigneeID on that second call, and a caller reading the answer is told the assignment came undone")
	}
}

// runTaskSuiteQuietly runs tasktest.RunService against the fake, optionally
// wrapped, and reports whether it passed. It runs in a testing.T of its own so
// a suite that fails on purpose does not fail the test watching it — the shape
// review 1's contenttest test uses and kit/porttest's own reporter uses.
func runTaskSuiteQuietly(t *testing.T, wrap func(contracts.Service) contracts.Service) bool {
	t.Helper()
	return testing.RunTests(
		func(pat, str string) (bool, error) { return true, nil },
		[]testing.InternalTest{{
			Name: "tasktest_RunService",
			F: func(inner *testing.T) {
				tasktest.RunService(inner, func(inner *testing.T, run func(tasktest.Fixture)) {
					ctx := tenancy.WithTenant(inner.Context(), acme)
					fake := tasktest.NewFake()
					var svc contracts.Service = fake
					if wrap != nil {
						svc = wrap(fake)
					}
					run(tasktest.Fixture{
						Ctx: ctx, Service: svc,
						Seed:      func(task *contracts.Task) uuid.UUID { return fake.Put(ctx, task) },
						Task:      func(id uuid.UUID) (contracts.Task, error) { return fake.Task(ctx, id) },
						Published: fake.Events.Names,
					})
				})
			},
		}})
}

// answersAnotherTask stores what the fake stores and lies the second time one
// task is assigned: the store holds the assignment, the answer says nobody owns
// the task and it is still open.
type answersAnotherTask struct {
	contracts.Service
	seen map[uuid.UUID]bool
}

func (a *answersAnotherTask) Assign(ctx context.Context, tx db.Tx[db.Tenant], id, assignee uuid.UUID) (*contracts.Task, error) {
	got, err := a.Service.Assign(ctx, tx, id, assignee)
	if err != nil || got == nil {
		return got, err
	}
	if a.seen[id] {
		lie := *got
		lie.Status = contracts.StatusOpen
		lie.AssigneeID = nil
		return &lie, nil
	}
	a.seen[id] = true
	return got, nil
}
