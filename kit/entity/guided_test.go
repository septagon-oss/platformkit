package entity_test

import (
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"testing"
	"time"
)

func validGuided() entity.GuidedSession {
	return entity.GuidedSession{ID: uuid.New(), Title: "Sequence", Steps: []entity.GuidedStep{{ID: uuid.New(), Title: "Step", Sets: []uuid.UUID{uuid.New()}, Items: []entity.GuidedItem{}}}, Progress: &entity.GuidedProgress{Revision: 1, Completed: []entity.GuidedCompletion{}}}
}
func TestGuidedReadRejectsIncoherentSnapshots(t *testing.T) {
	for name, change := range map[string]func(*entity.GuidedSession){
		"empty":             func(s *entity.GuidedSession) { s.Steps = nil },
		"blank title":       func(s *entity.GuidedSession) { s.Title = " " },
		"duplicate step":    func(s *entity.GuidedSession) { s.Steps = append(s.Steps, s.Steps[0]) },
		"empty sets":        func(s *entity.GuidedSession) { s.Steps[0].Sets = nil },
		"duplicate set":     func(s *entity.GuidedSession) { s.Steps[0].Sets = append(s.Steps[0].Sets, s.Steps[0].Sets[0]) },
		"timer":             func(s *entity.GuidedSession) { s.Steps[0].Timer = &entity.GuidedTimer{Name: "Wait", Seconds: 86401} },
		"target kind":       func(s *entity.GuidedSession) { s.Steps[0].Target = &entity.GuidedTarget{Kind: "minutes", Value: 1} },
		"between on single": func(s *entity.GuidedSession) { s.Steps[0].RestBetweenSeconds = 1 },
		"url picture":       func(s *entity.GuidedSession) { s.Picture = "https://example.test/picture.png" },
		"missing progress":  func(s *entity.GuidedSession) { s.Progress = nil },
		"wrong revision":    func(s *entity.GuidedSession) { s.Progress.Revision = 2 },
		"foreign receipt": func(s *entity.GuidedSession) {
			s.Progress.Revision = 2
			s.Progress.Completed = []entity.GuidedCompletion{{SessionID: uuid.New(), StepID: s.Steps[0].ID, SetID: s.Steps[0].Sets[0], Revision: 2, CompletedAt: time.Now().UTC()}}
		},
		"bad summary": func(s *entity.GuidedSession) {
			s.Progress.Revision = 2
			s.Progress.Finished = &entity.GuidedSummary{SessionID: s.ID, Revision: 2, TotalSets: 2, FinishedAt: time.Now().UTC(), Title: "Ended"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := validGuided()
			change(&s)
			if err := s.Validate(true); err == nil {
				t.Fatal("invalid snapshot accepted")
			}
		})
	}
	s := validGuided()
	if err := s.Validate(true); err != nil {
		t.Fatal(err)
	}
	s.Progress = nil
	if err := s.Validate(false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := s.Pair(-1); ok {
		t.Fatal("negative position accepted")
	}
	if _, _, ok := s.Pair(1); ok {
		t.Fatal("past-end position accepted")
	}
}
