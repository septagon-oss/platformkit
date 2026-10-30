package resource

// The longest pause a session may declare is written down in three places and
// decided in one. kit/entity's validation of a step admits a length; the case
// that walks every second count to the clock stops at a literal; and
// ui/screens/guided.go clamps the seconds a person adds on top of a rest inside
// the same literal. The two copies take the number from the validator by hand
// and nothing checks the copy: a ceiling widened there would leave both behind,
// so a person reloading at the longer pause would read a length no case ever
// asked the countdown about, and the screen's clamp, min(extra, 86400-seconds),
// would hand back a negative offer past it. This case asks the validator, rather
// than the copy, where a declared pause stops.

import (
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// longestPauseAPageAnswersFor is the last second count
// TestTheScriptCountsEveryPauseASessionMayDeclare carries to the countdown
// script, and the ceiling ui/screens/guided.go clamps a rest offer inside.
const longestPauseAPageAnswersFor = 86400

// TestTheLongestPauseAPageShowsIsTheLongestASessionMayDeclare requires every
// length the pages stop at — a rest after a step, a rest between two sets, a
// target counted in seconds — to be the longest its own validator admits: taken
// at longestPauseAPageAnswersFor and refused one second past it, so the range the
// page and the countdown are one across is the range a session can declare. A
// session with no progress record is what a module declares a read as, which is
// the shape validated here.
func TestTheLongestPauseAPageShowsIsTheLongestASessionMayDeclare(t *testing.T) {
	for _, pause := range []struct {
		name string
		take func(step *entity.GuidedStep, seconds int32)
	}{
		{"a rest after a step", func(step *entity.GuidedStep, seconds int32) {
			step.RestAfterSeconds = seconds
		}},
		{"a rest between sets", func(step *entity.GuidedStep, seconds int32) {
			step.Sets = []uuid.UUID{uuid.New(), uuid.New()}
			step.RestBetweenSeconds = seconds
		}},
		{"a target in seconds", func(step *entity.GuidedStep, seconds int32) {
			step.Target = &entity.GuidedTarget{Kind: "seconds", Value: seconds}
		}},
	} {
		for _, asked := range []struct {
			seconds int32
			admits  bool
		}{
			{longestPauseAPageAnswersFor, true},
			{longestPauseAPageAnswersFor + 1, false},
		} {
			session := entity.GuidedSession{ID: uuid.New(), Title: "Sequence", Steps: []entity.GuidedStep{{ID: uuid.New(), Title: "Step", Sets: []uuid.UUID{uuid.New()}, Items: []entity.GuidedItem{}}}}
			pause.take(&session.Steps[0], asked.seconds)
			err := session.Validate(false)
			if asked.admits && err != nil {
				t.Errorf("%s of %d seconds: the validator refuses the very length the pages write and count: %v", pause.name, asked.seconds, err)
			}
			if !asked.admits && err == nil {
				t.Errorf("%s of %d seconds: the validator admits past the %d the pages copy, so the case that walks every declared pause stops short of one a person can be shown", pause.name, asked.seconds, longestPauseAPageAnswersFor)
			}
		}
	}
}
