package entity

import (
	"github.com/google/uuid"
	"time"
)

// GuidedSession is an immutable ordered snapshot; Progress is nil for a read with no commands.
type GuidedSession struct {
	ID       uuid.UUID       `json:"id"`
	Title    string          `json:"title"`
	Picture  Picture         `json:"picture"`
	Steps    []GuidedStep    `json:"steps"`
	Progress *GuidedProgress `json:"progress"`
}

// GuidedStep has stable set identities; zero rest means no rest.
type GuidedStep struct {
	ID                 uuid.UUID     `json:"id"`
	Title              string        `json:"title"`
	Text               string        `json:"text"`
	Picture            Picture       `json:"picture"`
	Items              []GuidedItem  `json:"items"`
	Target             *GuidedTarget `json:"target"`
	Timer              *GuidedTimer  `json:"timer"`
	Sets               []uuid.UUID   `json:"sets"`
	RestBetweenSeconds int32         `json:"restBetweenSeconds"`
	RestAfterSeconds   int32         `json:"restAfterSeconds"`
}

// GuidedItem is plain display content, with an optional composed picture.
type GuidedItem struct {
	Label    string  `json:"label"`
	Quantity string  `json:"quantity"`
	Picture  Picture `json:"picture"`
}

// GuidedTarget is a positive repetitions count or a duration in seconds.
type GuidedTarget struct {
	Kind  string `json:"kind"` // "repetitions" or "seconds"
	Value int32  `json:"value"`
}

// GuidedTimer names an advisory countdown; it never records completion.
type GuidedTimer struct {
	Name    string `json:"name"`
	Seconds int32  `json:"seconds"`
}

// GuidedProgress is the module-owned completed prefix and optional terminal summary.
type GuidedProgress struct {
	Revision  int64              `json:"revision"`
	Completed []GuidedCompletion `json:"completed"`
	Finished  *GuidedSummary     `json:"finished"`
}

// GuidedCompletion is the immutable receipt for one accepted set.
type GuidedCompletion struct {
	SessionID   uuid.UUID `json:"sessionId"`
	StepID      uuid.UUID `json:"stepId"`
	SetID       uuid.UUID `json:"setId"`
	Revision    int64     `json:"revision"`
	CompletedAt time.Time `json:"completedAt"`
}

// GuidedSummary is the module-owned result of explicitly finishing, including an empty prefix.
type GuidedSummary struct {
	SessionID     uuid.UUID    `json:"sessionId"`
	Revision      int64        `json:"revision"`
	FinishedAt    time.Time    `json:"finishedAt"`
	CompletedSets int32        `json:"completedSets"`
	TotalSets     int32        `json:"totalSets"`
	Title         string       `json:"title"`
	Text          string       `json:"text"`
	Facts         []GuidedFact `json:"facts"`
}

// GuidedFact is one plain-text label and value in the finish summary.
type GuidedFact struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// CompleteGuidedSet binds a stable pair to the revision the actor observed.
type CompleteGuidedSet struct {
	StepID     uuid.UUID `json:"stepId"`
	SetID      uuid.UUID `json:"setId"`
	IfRevision int64     `json:"ifRevision"`
}

// FinishGuidedSession quotes the observed revision without completing missing sets.
type FinishGuidedSession struct {
	IfRevision int64 `json:"ifRevision"`
}
