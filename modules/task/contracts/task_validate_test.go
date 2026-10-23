package contracts_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

func TestTaskValidateReportsEveryViolationAtOnce(t *testing.T) {
	t.Parallel()
	task := contracts.Task{Title: "  ", Status: "bogus"}
	got, ok := errors.AsType[fault.Violations](task.Validate(context.Background()))
	if !ok || len(got) != 2 {
		t.Fatalf("violations = %v, %v", got, ok)
	}
	if got[0] != (fault.Violation{Location: "title", Code: "required", Message: "a task needs a title"}) {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].Location != "status" || got[1].Code != "not_one_of" || got[1].Message != `status "bogus" is not a lifecycle state` {
		t.Errorf("second = %+v", got[1])
	}
}

func TestTaskValidateKeepsItsSentencesAndErrInvalid(t *testing.T) {
	t.Parallel()
	now := time.Now()
	cases := map[string]struct {
		task     contracts.Task
		location string
		message  string
	}{
		"deadline": {contracts.Task{Title: "x", SLABreached: true}, "slaBreached", "a breach needs the deadline it broke"},
		"resolved": {contracts.Task{Title: "x", Status: contracts.StatusResolved}, "resolvedAt", "a resolved task needs a resolution time"},
		"stray":    {contracts.Task{Title: "x", ResolvedAt: &now}, "resolvedAt", "a open task cannot have a resolution time"},
		"priority": {contracts.Task{Title: "x", Priority: "p"}, "priority", `priority "p" is not a priority`},
	}
	for name, c := range cases {
		err := c.task.Validate(context.Background())
		got, ok := errors.AsType[fault.Violations](err)
		if !ok || len(got) != 1 || got[0].Location != c.location || got[0].Message != c.message {
			t.Errorf("%s: %v", name, err)
		}
		if !errors.Is(err, fault.ErrInvalid) {
			t.Errorf("%s: not ErrInvalid", name)
		}
	}
	ok := contracts.Task{Title: "x"}
	if err := ok.Validate(context.Background()); err != nil {
		t.Errorf("valid task refused: %v", err)
	}
}
