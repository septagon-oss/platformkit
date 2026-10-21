// Package contracts specifies tenant-scoped group decisions and ballot rules.
// Services use the caller's tenant transaction; errors return zero results and
// require rollback. Persistence and transport are supplied by the module.
package contracts

import (
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
)

const (
	PermissionRead   = "decide:read"
	PermissionManage = "decide:manage"
	PermissionVote   = "decide:vote"
	StatusOpen       = "open"
	StatusClosed     = "closed"
)

// Value is a ballot choice. A nil *Value is a retained undo tombstone.
type Value string

const (
	Yes    Value = "yes"
	Strong Value = "strong"
	No     Value = "no"
	Skip   Value = "skip"
)

// Question is the creator-owned aggregate. Revision covers structural changes,
// not ballots; AcceptingVotes also consults ClosesAt through an explicit clock.
type Question struct {
	entity.Base
	Title            string     `json:"title"`
	Description      string     `json:"description,omitempty"`
	ClosesAt         *time.Time `json:"closesAt,omitempty"`
	AllowLinkJoin    bool       `json:"allowLinkJoin"`
	Status           string     `json:"status" readOnly:"true"`
	CreatorID        uuid.UUID  `json:"creatorId" readOnly:"true"`
	Revision         int64      `json:"revision" readOnly:"true"`
	ClosedAt         *time.Time `json:"closedAt,omitempty" readOnly:"true"`
	InviteGeneration int64      `json:"-"`
	InviteExpiresAt  *time.Time `json:"-"`
}

func (Question) TableName() string { return "decide_questions" }

// Option is an ordered choice. Removed options and the lifetime match latch are
// retained; they never appear in an active option list or tally after removal.
type Option struct {
	entity.Base
	QuestionID     uuid.UUID  `json:"questionId"`
	Title          string     `json:"title"`
	ImageID        *uuid.UUID `json:"imageId,omitempty" ui:"present:image"`
	Note           string     `json:"note,omitempty"`
	Link           string     `json:"link,omitempty"`
	Position       int64      `json:"position"`
	Revision       int64      `json:"revision" readOnly:"true"`
	RemovedAt      *time.Time `json:"-"`
	FirstMatchedAt *time.Time `json:"-"`
}

func (Option) TableName() string { return "decide_options" }

// Participant binds an existing workspace person to one question permanently.
type Participant struct {
	entity.Base
	QuestionID uuid.UUID `json:"questionId"`
	PersonID   uuid.UUID `json:"personId"`
	JoinedAt   time.Time `json:"joinedAt"`
	Revision   int64     `json:"revision" readOnly:"true"`
}

func (Participant) TableName() string { return "decide_participants" }

// Vote is one person's current choice and monotonic revision for one option.
// Undoing the first choice retains a nil Value so old revisions cannot recur.
type Vote struct {
	entity.Base
	QuestionID uuid.UUID `json:"questionId"`
	OptionID   uuid.UUID `json:"optionId"`
	PersonID   uuid.UUID `json:"personId"`
	Value      *Value    `json:"value"`
	At         time.Time `json:"at"`
	Revision   int64     `json:"revision" readOnly:"true"`
}

func (Vote) TableName() string { return "decide_votes" }

// Snapshot is the complete aggregate loaded under its question lock. The SQL
// adapter supplies tenant-scoped rows; decisions refuse inconsistent inputs.
type Snapshot struct {
	Question     Question
	Options      []Option
	Participants []Participant
	Votes        []Vote
}

// Actor is current authority resolved by Access, never accepted from a body.
type Actor struct {
	PersonID           uuid.UUID
	Read, Manage, Vote bool
}

// VoteInput addresses only the current actor's ballot. IfRevision is zero for
// physical absence, otherwise the actor's observed ballot revision.
type VoteInput struct {
	QuestionID uuid.UUID `json:"questionId"`
	OptionID   uuid.UUID `json:"optionId"`
	Value      Value     `json:"value"`
	IfRevision int64     `json:"ifRevision"`
}

// Groups includes all four choices, including their zero counts/empty arrays.
type Groups[T any] struct {
	Yes    T `json:"yes"`
	Strong T `json:"strong"`
	No     T `json:"no"`
	Skip   T `json:"skip"`
}

// Result describes one active option. Matched requires at least one ballot and
// no no/skip; missing ballots do not veto agreement among actual voters.
type Result struct {
	Option  Option              `json:"option"`
	Score   int64               `json:"score"`
	Counts  Groups[int64]       `json:"counts"`
	Voters  Groups[[]uuid.UUID] `json:"voters"`
	Matched bool                `json:"matched"`
}

// Results is one locked read, sorted by score descending, position then ID.
// QuestionRevision describes structure and is not a ballot cache validator.
type Results struct {
	QuestionID       uuid.UUID `json:"questionId"`
	QuestionRevision int64     `json:"questionRevision"`
	ObservedAt       time.Time `json:"observedAt"`
	AcceptingVotes   bool      `json:"acceptingVotes"`
	Options          []Result  `json:"options"`
}

// Card supplies the actor's current ballot revision even for an undo tombstone.
type Card struct {
	Option
	VoteRevision int64 `json:"voteRevision" gorm:"-" readOnly:"true"`
	Unvoted      bool  `json:"unvoted" gorm:"-" readOnly:"true"`
}

// Deck is an actor-scoped queue and consistent progress. Closed questions have
// an empty Cards slice while preserving Total and Voted.
type Deck struct {
	QuestionID     uuid.UUID `json:"questionId"`
	AcceptingVotes bool      `json:"acceptingVotes"`
	Total          int64     `json:"total"`
	Voted          int64     `json:"voted"`
	Remaining      int64     `json:"remaining"`
	Cards          []Card    `json:"cards"`
}
