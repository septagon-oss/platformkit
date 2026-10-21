package contracts

import (
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/events"
)

const (
	EventVoted   = "decide.voted"
	EventMatched = "decide.matched"
	EventClosed  = "decide.closed"
)

// Events is the complete event vocabulary; no generic CRUD writes are mounted.
var Events = []events.Declared{
	events.Declare[Voted](EventVoted),
	events.Declare[Matched](EventMatched),
	events.Declare[Closed](EventClosed),
}

// Voted is an actual ballot change; null Value means undo restored absence.
type Voted struct {
	QuestionID   uuid.UUID `json:"questionId"`
	OptionID     uuid.UUID `json:"optionId"`
	PersonID     uuid.UUID `json:"personId"`
	Previous     *Value    `json:"previous"`
	Value        *Value    `json:"value"`
	VoteRevision int64     `json:"voteRevision"`
	At           time.Time `json:"at"`
}

// Matched snapshots agreement and the recipient roster at the first match only.
// Both arrays are canonical UUID byte order; later joins receive no old notices.
type Matched struct {
	QuestionID uuid.UUID   `json:"questionId"`
	OptionID   uuid.UUID   `json:"optionId"`
	Voters     []uuid.UUID `json:"voters"`
	Recipients []uuid.UUID `json:"recipients"`
	At         time.Time   `json:"at"`
}

// Closed records a persisted transition. ClosedBy is nil only for deadline work.
type Closed struct {
	QuestionID       uuid.UUID  `json:"questionId"`
	QuestionRevision int64      `json:"questionRevision"`
	ClosedBy         *uuid.UUID `json:"closedBy"`
	Reason           string     `json:"reason"`
	At               time.Time  `json:"at"`
}
