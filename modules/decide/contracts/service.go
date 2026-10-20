package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
)

// Access re-resolves active identity and current grants/credential ceilings after
// the question lock. It also checks every target as an active same-tenant person.
// Nil targets means only the actor. Implementations acquire the whole identity/
// revocation lock set in canonical owner order and retain it until commit.
type Access interface {
	Current(context.Context, db.Tx[db.Tenant], []uuid.UUID) (Actor, error)
}

// Clock is required and sampled after authoritative locks; services normalize UTC.
type Clock func() time.Time

// InviteKey is a composition-supplied signing secret of at least 32 random bytes.
// Constructors copy it; changing the key invalidates outstanding credentials.
type InviteKey []byte

// OptionInput contains only creator-authored fields; IDs and timestamps are owned
// by the command. Text is plain UTF-8; Link is an optional absolute HTTP(S) URL.
type OptionInput struct {
	Title    string     `json:"title"`
	ImageID  *uuid.UUID `json:"imageId,omitempty"`
	Note     string     `json:"note,omitempty"`
	Link     string     `json:"link,omitempty"`
	Position int64      `json:"position"`
}

// OpenInput creates an open question with at least one option and its creator
// membership. A closing time must be future; link joining defaults to false.
type OpenInput struct {
	Title         string        `json:"title"`
	Description   string        `json:"description,omitempty"`
	ClosesAt      *time.Time    `json:"closesAt,omitempty"`
	AllowLinkJoin bool          `json:"allowLinkJoin"`
	Options       []OptionInput `json:"options"`
}

// QuestionInput quotes the structural revision observed by its creator.
type QuestionInput struct {
	QuestionID uuid.UUID `json:"questionId"`
	IfRevision int64     `json:"ifRevision"`
}

type AddOptionInput struct {
	QuestionInput
	Option OptionInput `json:"option"`
}

type RemoveOptionInput struct {
	QuestionInput
	OptionID uuid.UUID `json:"optionId"`
}

type AddParticipantInput struct {
	QuestionInput
	PersonID uuid.UUID `json:"personId"`
}

type IssueInviteInput struct {
	QuestionInput
	ExpiresAt time.Time `json:"expiresAt"`
}

// JoinInput carries a credential, never a target person. IfRevision is zero;
// an already admitted actor is a no-op only after revalidating this credential.
type JoinInput struct {
	QuestionID uuid.UUID `json:"questionId"`
	Token      string    `json:"token"`
	IfRevision int64     `json:"ifRevision"`
}

type UndoVoteInput struct {
	Token string `json:"token"`
}

// InviteClaims is trusted output of MAC verification, not a public join input.
// Its generation and whole-second expiry must equal the locked question's values.
type InviteClaims struct {
	TenantID   uuid.UUID
	QuestionID uuid.UUID
	Generation int64
	ExpiresAt  time.Time
}

// UndoClaims is trusted output of undo MAC verification. Previous is nil when
// rewind restores absence; PostRevision binds the token to one accepted cast.
type UndoClaims struct {
	TenantID     uuid.UUID
	QuestionID   uuid.UUID
	OptionID     uuid.UUID
	PersonID     uuid.UUID
	PostRevision int64
	Previous     *Value
	ExpiresAt    time.Time
}

type OptionChange struct {
	Question Question `json:"question"`
	Option   Option   `json:"option"`
}

type ParticipantChange struct {
	Question    Question    `json:"question"`
	Participant Participant `json:"participant"`
}

// InviteReply is private command output. Ordinary reads/events never carry Token.
type InviteReply struct {
	QuestionRevision int64     `json:"questionRevision"`
	Token            string    `json:"token"`
	ExpiresAt        time.Time `json:"expiresAt"`
}

// VoteReply carries an authoritative rewind credential for accepted casts.
// Undo responses omit it; it expires after 15 minutes or at the question deadline.
type VoteReply struct {
	Vote      Vote   `json:"vote"`
	UndoToken string `json:"undoToken,omitempty"`
}

// QuestionQuery filters stored lifecycle status, with optional empty Status.
// Offset defaults to zero; Limit zero means 50, with a maximum of 200.
type QuestionQuery struct {
	Status string `json:"status,omitempty"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// QuestionPage counts only actor-visible questions before applying pagination.
type QuestionPage struct {
	Items []Question `json:"items"`
	Total int64      `json:"total"`
}

// Service is the intended authoritative command/read boundary. Every operation
// resolves tx's tenant and current Access. Errors return zero and require caller
// rollback. Mutations recheck scope, state and quoted revision under the question
// lock; generic CRUD writes are not part of this contract. See ../README.md.
type Service interface {
	Open(context.Context, db.Tx[db.Tenant], OpenInput) (Question, error)
	Close(context.Context, db.Tx[db.Tenant], QuestionInput) (Question, error)
	AddOption(context.Context, db.Tx[db.Tenant], AddOptionInput) (OptionChange, error)
	RemoveOption(context.Context, db.Tx[db.Tenant], RemoveOptionInput) (Question, error)
	AddParticipant(context.Context, db.Tx[db.Tenant], AddParticipantInput) (ParticipantChange, error)
	IssueInvite(context.Context, db.Tx[db.Tenant], IssueInviteInput) (InviteReply, error)
	RevokeInvite(context.Context, db.Tx[db.Tenant], QuestionInput) (Question, error)
	Join(context.Context, db.Tx[db.Tenant], JoinInput) (ParticipantChange, error)
	Vote(context.Context, db.Tx[db.Tenant], VoteInput) (VoteReply, error)
	UndoVote(context.Context, db.Tx[db.Tenant], UndoVoteInput) (VoteReply, error)
	Question(context.Context, db.Tx[db.Tenant], uuid.UUID) (Question, error)
	Questions(context.Context, db.Tx[db.Tenant], QuestionQuery) (QuestionPage, error)
	Options(context.Context, db.Tx[db.Tenant], uuid.UUID) ([]Option, error)
	Participants(context.Context, db.Tx[db.Tenant], uuid.UUID) ([]Participant, error)
	Results(context.Context, db.Tx[db.Tenant], uuid.UUID) (Results, error)
	Deck(context.Context, db.Tx[db.Tenant], uuid.UUID) (Deck, error)
	Export(context.Context, db.Tx[db.Tenant], uuid.UUID) (Export, error)
}

// Export contains current domain rows and results, with private fields excluded
// by their JSON tags. It has the same authority and snapshot lock as Results.
type Export struct {
	Question     Question      `json:"question"`
	Options      []Option      `json:"options"`
	Participants []Participant `json:"participants"`
	Votes        []Vote        `json:"votes"`
	Results      Results       `json:"results"`
}

// Notifier writes participant notices in the event consumer's transaction.
// A failed batch rolls back the existing delivery claim and all its notices.
type Notifier interface {
	Matched(context.Context, db.Tx[db.Tenant], Matched) error
}
