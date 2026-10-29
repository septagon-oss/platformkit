package internal

import (
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/tours/contracts"
)

func TestStaleTourActionDoesNotChangeRun(t *testing.T) {
	run := &contracts.Run{Revision: 4, Status: "active", CurrentStep: "intro", Done: []string{}, Skipped: []string{}}
	before := *run
	err := transition(run, contracts.Action{Kind: "next", Step: "intro", ExpectedRevision: 3, At: time.Now().UTC()}, []string{"intro", "finish"})
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("transition error = %v, want conflict", err)
	}
	if run.Revision != before.Revision || run.Status != before.Status || run.CurrentStep != before.CurrentStep || len(run.Done) != 0 {
		t.Fatalf("refused action changed run: %#v", run)
	}
}

func TestTourLaterResumesAtItsDueTime(t *testing.T) {
	at := time.Date(2026, 10, 29, 11, 0, 0, 0, time.UTC)
	run := &contracts.Run{Revision: 2, Status: "active", CurrentStep: "intro", Done: []string{}, Skipped: []string{}}
	if err := transition(run, contracts.Action{Kind: "later", ExpectedRevision: 2, At: at}, []string{"intro", "finish"}); err != nil {
		t.Fatal(err)
	}
	if err := transition(run, contracts.Action{Kind: "resume", ExpectedRevision: 3, At: at.Add(24*time.Hour - time.Second)}, []string{"intro", "finish"}); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("early resume error = %v, want conflict", err)
	}
	if run.Revision != 3 || run.Status != "later" {
		t.Fatalf("early resume changed run: %#v", run)
	}
	if err := transition(run, contracts.Action{Kind: "resume", ExpectedRevision: 3, At: at.Add(24 * time.Hour)}, []string{"intro", "finish"}); err != nil {
		t.Fatal(err)
	}
	if run.Status != "active" || run.CurrentStep != "intro" || run.Revision != 4 {
		t.Fatalf("resumed run = %#v", run)
	}
}

func TestTourCompletionLatchesAtFinalStep(t *testing.T) {
	at := time.Now().UTC()
	run := &contracts.Run{Revision: 1, Status: "active", CurrentStep: "only", Done: []string{}, Skipped: []string{}}
	if err := transition(run, contracts.Action{Kind: "next", Step: "only", ExpectedRevision: 1, At: at}, []string{"only"}); err != nil {
		t.Fatal(err)
	}
	if run.Status != "completed" || run.CompletedAt == nil || !run.CompletedAt.Equal(at) {
		t.Fatalf("completed run = %#v", run)
	}
}
