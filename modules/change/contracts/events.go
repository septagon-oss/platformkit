package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// The four transitions, one event each. There is no "changed" event: a proposal
// that moved without one of these four names on it is a bug, and the state
// machine in ../internal refuses it rather than reporting it.
const (
	EventProposed  = "change.proposal_proposed"
	EventReviewed  = "change.proposal_reviewed"
	EventApplied   = "change.proposal_applied"
	EventWithdrawn = "change.proposal_withdrawn"
)

// Events is every event this module emits, for the manifest.
var Events = []events.Declared{
	events.Declare[Proposed](EventProposed),
	events.Declare[Reviewed](EventReviewed),
	events.Declare[Applied](EventApplied),
	events.Declare[Withdrawn](EventWithdrawn),
}

// Proposed is the payload of the first transition: the diff, named by its digest,
// against the subject's revision at the moment it was read under lock. It carries
// the diff itself because the audit trail is the evidence, and a trail that said
// only "somebody proposed something" would need the proposal row to be readable
// forever to answer what was proposed.
type Proposed struct {
	ProposalID    uuid.UUID `json:"proposalId"`
	SubjectModule string    `json:"subjectModule"`
	SubjectEntity string    `json:"subjectEntity"`
	SubjectID     uuid.UUID `json:"subjectId"`
	BaseRevision  int64     `json:"baseRevision"`
	DiffDigest    string    `json:"diffDigest"`
	Diff          Diff      `json:"diff"`
	Summary       string    `json:"summary"`
	Proposer      uuid.UUID `json:"proposer"`
	At            time.Time `json:"at"`
}

// Reviewed is the decision. The verdict is carried with the reviewer rather than
// only as a state, because "who said yes" is the question the trail is asked.
type Reviewed struct {
	ProposalID uuid.UUID `json:"proposalId"`
	Verdict    string    `json:"verdict"`
	Reviewer   uuid.UUID `json:"reviewer"`
	Proposer   uuid.UUID `json:"proposer"`
	Comment    string    `json:"comment,omitempty"`
	State      string    `json:"state"`
	At         time.Time `json:"at"`
}

// Applied is the write that happened: the subject's revision before and after, so
// the trail alone answers "this change produced that revision" without a join to
// a table retention may have moved on from.
type Applied struct {
	ProposalID    uuid.UUID `json:"proposalId"`
	SubjectModule string    `json:"subjectModule"`
	SubjectEntity string    `json:"subjectEntity"`
	SubjectID     uuid.UUID `json:"subjectId"`
	Reviewer      uuid.UUID `json:"reviewer"`
	Proposer      uuid.UUID `json:"proposer"`
	BaseRevision  int64     `json:"baseRevision"`
	NewRevision   int64     `json:"newRevision"`
	DiffDigest    string    `json:"diffDigest"`
	At            time.Time `json:"at"`
}

// Withdrawn is the proposer taking it back, with the state it came from — which
// is the difference between abandoning an idea and un-sending one that had been
// approved, and the trail keeps both.
type Withdrawn struct {
	ProposalID uuid.UUID `json:"proposalId"`
	Proposer   uuid.UUID `json:"proposer"`
	FromState  string    `json:"fromState"`
	At         time.Time `json:"at"`
}
