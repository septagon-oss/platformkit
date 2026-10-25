package tasktest_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

// The fake decides creation by calling the service's own decision rather than a
// copy of it, so the two worlds cannot answer one input differently. These cases
// hold that in place: they run one input through both and compare the two answers,
// so a second creation decision re-appearing on either side reddens a case rather
// than leaving two suites green over two rulebooks.

// TestTheFakeDecidesCreationAsTheServiceDoes walks the inputs the creator suite
// refuses, plus the three it accepts, and asks both worlds about each.
//
// An input carrying an assignee is not in this table. Acknowledging the task and
// publishing the assignment are the lifecycle's own decision, which the service
// runs through its Assign command and the fake mirrors inline; the shared
// conformance suite runs that behaviour against both worlds, and this case stops
// where the shared call does.
func TestTheFakeDecidesCreationAsTheServiceDoes(t *testing.T) {
	ctx := tenancy.WithTenant(t.Context(), acme)
	nobody, zero := uuid.Nil, time.Time{}
	past := time.Now().Add(-time.Hour)
	for _, row := range []struct {
		name string
		in   contracts.CreateTask
	}{
		{name: "an empty title", in: contracts.CreateTask{Title: ""}},
		{name: "a blank title", in: contracts.CreateTask{Title: " \t "}},
		{name: "a title of 201 chars", in: contracts.CreateTask{Title: strings.Repeat("é", 201)}},
		{name: "an unknown status", in: contracts.CreateTask{Title: "x", Status: "paused"}},
		{name: "an unknown priority", in: contracts.CreateTask{Title: "x", Priority: "urgent"}},
		{name: "a resolved task", in: contracts.CreateTask{Title: "x", Status: contracts.StatusResolved}},
		{name: "a closed task", in: contracts.CreateTask{Title: "x", Status: contracts.StatusClosed}},
		{name: "a source of 41 chars", in: contracts.CreateTask{Title: "x", Source: strings.Repeat("s", 41)}},
		{name: "a sourceRef of 121", in: contracts.CreateTask{Title: "x", SourceRef: strings.Repeat("r", 121)}},
		{name: "an assignee of nobody", in: contracts.CreateTask{Title: "x", AssigneeID: &nobody}},
		{name: "a zero deadline", in: contracts.CreateTask{Title: "x", SLADeadline: &zero}},
		{name: "a title needing its space trimmed", in: contracts.CreateTask{Title: "  chiller  "}},
		{name: "a past deadline", in: contracts.CreateTask{Title: "chiller", SLADeadline: &past}},
		{name: "a soft target and a real deadline", in: contracts.CreateTask{Title: "x", DueAt: &past, SLADeadline: &past}},
	} {
		t.Run(row.name, func(t *testing.T) {
			made, madeErr := internal.NewTask(ctx, row.in)
			fake, fakeErr := tasktest.NewFake().Create(ctx, db.Tx[db.Tenant]{}, row.in)
			compareRefusal(t, madeErr, fakeErr)
			if madeErr != nil {
				if made != nil || fake != nil {
					t.Errorf("a refused create answered a task: service %v, fake %v", made, fake)
				}
				return
			}
			if decided(made) != decided(fake) {
				t.Errorf("the fake made %s where the service makes %s", decided(fake), decided(made))
			}
		})
	}
}

// compareRefusal asks whether the two worlds said the same thing about the same
// input: the same error the caller can act on, and the same sentence, so a
// consumer that reads the message into a form field sees what production says.
func compareRefusal(t *testing.T, madeErr, fakeErr error) {
	t.Helper()
	if (madeErr == nil) != (fakeErr == nil) {
		t.Fatalf("the service refused with %v and the fake refused with %v", madeErr, fakeErr)
	}
	if madeErr == nil {
		return
	}
	for _, sentinel := range []error{crud.ErrInvalid, crud.ErrConflict, crud.ErrNotFound, tenancy.ErrPolicyDenied} {
		if errors.Is(madeErr, sentinel) != errors.Is(fakeErr, sentinel) {
			t.Errorf("the service refused %v and the fake refused %v: they disagree about %s", madeErr, fakeErr, sentinel)
		}
	}
	if madeErr.Error() != fakeErr.Error() {
		t.Errorf("the fake says %q where the service says %q", fakeErr, madeErr)
	}
}

// decided is the half of the answer the shared decision writes: what the input
// became. The identity, revision, breach flag and assignee are left out on purpose
// — the service leaves them for its transaction and the fake mirrors its lifecycle
// commands, which the shared conformance suite is for.
func decided(task *contracts.Task) string {
	if task == nil {
		return "nothing"
	}
	return fmt.Sprintf("title=%q status=%s priority=%s source=%q sourceRef=%q dueAt=%s slaDeadline=%s",
		task.Title, task.Status, task.Priority, task.Source, task.SourceRef, when(task.DueAt), when(task.SLADeadline))
}

func when(at *time.Time) string {
	if at == nil {
		return "unsaid"
	}
	return at.UTC().Format(time.RFC3339Nano)
}
