// Package contracts is everything another module, an app or a test may know
// about change control: the proposal, its states, the commands that move it, the
// events it publishes, the permissions that guard them and the two ports a
// subject and a gate are reached through. The implementation is in ../internal.
//
// The object is a proposal: one change to one entity in one tenant, put forward
// for somebody else to look at. What this module owns is the *object* — a row
// with a digest of the exact bytes, a state machine, a proposer the actor was
// forced to be, and a write that only happens once and only after a different
// account said yes. It owns no policy about which writes need that: the
// composition says which of its operations a gate refuses, and the module that
// owns the row says how to lock and save it. That separation is the whole point
// of making it a thing rather than a pattern: the reviewable record, the
// four-eyes rule and the stale-base refusal are written once here, and every
// place that needs them points at this instead of writing them again.
package contracts

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// The five states. A terminal one has no way out: a declined proposal is not
// re-opened, it is disagreed with and proposed again as a new row, so the record
// of the first opinion survives.
const (
	StateProposed  = "proposed"
	StateApproved  = "approved"
	StateDeclined  = "declined"
	StateWithdrawn = "withdrawn"
	StateApplied   = "applied"
)

var states = []string{StateProposed, StateApproved, StateDeclined, StateWithdrawn, StateApplied}

// The two verdicts. The empty verdict is not a third one: it is the state of
// having been proposed and not yet decided.
const (
	VerdictApproved = "approved"
	VerdictDeclined = "declined"
)

// MaxSummary bounds the one line a proposal carries: enough for "raise the fee
// floor to 4%", not enough for the argument, which belongs in the comment on a
// verdict rather than in the subject line of every screen that lists proposals.
const MaxSummary = 200

// The errors a caller can act on, beyond crud's three. Each says what is wrong in
// terms of what could be done next:
//
//	ErrSelfReview        immutable for the person holding the request: no edit to it
//	                     makes it true. The cure is a second account.
//	ErrStaleBase         correctable by proposing against the revision the subject is
//	                     on now, which is what the refusal says it was.
//	ErrUnsupportedSubject correctable by whoever composes the application, who is the
//	                     only person who can put the subject's binding back.
//
// They are values rather than statuses because the mapping to a status happens
// once, in kit/rest's Fault, and a module that invents its own status is a module
// whose refusal a client cannot predict.
var (
	ErrSelfReview         = fmt.Errorf("change: the person who proposed a change cannot be the one who decides it")
	ErrStaleBase          = fmt.Errorf("change: the subject has moved since this diff was made")
	ErrUnsupportedSubject = fmt.Errorf("change: this composition applies no proposals for that subject")
)

// Proposal is one change put forward for somebody else to look at.
//
// It is the row that answers "who wanted this, who said yes, against which exact
// bytes, and did it happen" — which is a question about four facts, none of which
// survives a comment on a pull request or a column that stores "approved by a
// second person" once two people hold the grant.
type Proposal struct {
	crud.Base

	// SubjectModule, SubjectEntity and SubjectID name the row this diff would
	// alter. They are three columns and no foreign key, for the reason modules/task
	// gives for Source: a foreign key into another module's table is a coupling the
	// module boundary does not allow. SubjectID is uuid.Nil for an entity with one
	// row per tenant, which is what site settings is.
	SubjectModule string    `json:"subjectModule" doc:"The module that owns the row" example:"site"`
	SubjectEntity string    `json:"subjectEntity" doc:"The entity inside it" example:"settings"`
	SubjectID     uuid.UUID `json:"subjectId" format:"uuid" doc:"The row, or nil for a one-per-tenant entity"`
	// BaseRevision is the subject's revision at the moment the diff was made. The
	// service reads it under the subject's row lock rather than taking it from the
	// body, because a base revision a caller could send is a base revision nobody
	// can rely on.
	BaseRevision int64 `json:"baseRevision" readOnly:"true" doc:"The subject's revision this diff was made against"`

	// Diff is the change, and DiffDigest is the digest of its canonical bytes. Both
	// are stored because the diff is what an apply runs and the digest is what a
	// verdict was about — and the pair is only evidence if the bytes in this column
	// still digest to the column beside it, which is why the column is text: jsonb
	// stores a number as a numeric and would answer "100" where the reviewed bytes
	// said "1e2". See migrations/000038 and Diff.Value.
	Diff       Diff   `json:"diff" gorm:"type:text;not null" required:"true" doc:"The change, as an RFC 7386 merge patch"`
	DiffDigest string `json:"diffDigest" readOnly:"true" doc:"sha256 of the canonical diff, which is what a verdict is about"`
	Summary    string `json:"summary" maxLength:"200" required:"true" doc:"One line on what the change is"`

	// Proposer is the actor the context carried at the moment of proposing, and
	// nothing else: NewProposal has no field for it and a body that sends one is
	// refused, not ignored, because a self-review with better manners is still a
	// self-review.
	Proposer uuid.UUID `json:"proposer" readOnly:"true" doc:"Who put it forward, from their own credentials"`

	// Reviewer, Verdict and Comment are the decision. They stay after the apply
	// because the record is the point, and ReviewedAt outlives Updated_at, which
	// moves again when the change is applied.
	Reviewer   *uuid.UUID `json:"reviewer,omitempty" readOnly:"true" format:"uuid" doc:"Who decided"`
	Verdict    string     `json:"verdict" enum:",approved,declined" readOnly:"true" doc:"The decision, empty until one is made"`
	Comment    string     `json:"comment,omitempty" maxLength:"2000" doc:"Why, in the reviewer's words"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty" readOnly:"true" doc:"When the decision was made"`

	// AppliedRevision is the subject's revision this proposal produced, and zero
	// means it has not been applied. There is no applied_at: applied is terminal,
	// so updated_at is exactly that moment.
	AppliedRevision int64 `json:"appliedRevision" readOnly:"true" doc:"The subject revision this proposal caused"`

	State string `json:"state" enum:"proposed,approved,declined,withdrawn,applied" readOnly:"true" doc:"Where it is in the lifecycle"`
	// Revision is this row's own optimistic revision, from 1. Every mutation
	// rechecks the caller's ExpectedRevision against it inside the transaction, so
	// two reviewers cannot both decide and neither notices.
	Revision int64 `json:"revision" readOnly:"true" doc:"This row's own revision, which every command rechecks"`
}

// TableName pins the table, so the entity and migrations/000038 agree.
func (Proposal) TableName() string { return "change_proposals" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through.
func (p *Proposal) Validate(context.Context) error {
	switch {
	case p.SubjectModule == "":
		return fmt.Errorf("a proposal names the module whose row it would change")
	case p.SubjectEntity == "":
		return fmt.Errorf("a proposal names the entity inside it")
	case p.Diff == nil:
		return fmt.Errorf("a proposal without a diff asks for nothing")
	case utf8.RuneCountInString(p.Summary) == 0 || utf8.RuneCountInString(p.Summary) > MaxSummary:
		return fmt.Errorf("a proposal needs a summary of at most %d characters", MaxSummary)
	case p.Proposer == uuid.Nil:
		return fmt.Errorf("a proposal needs the person who made it")
	case !slices.Contains(states, p.State):
		return fmt.Errorf("state %q is not a state a proposal is in", p.State)
	case p.Verdict != "" && p.Verdict != VerdictApproved && p.Verdict != VerdictDeclined:
		return fmt.Errorf("%q is not a verdict", p.Verdict)
	case p.Revision < 1:
		return fmt.Errorf("a proposal's own revision starts at 1")
	}
	return nil
}

// Open reports whether this proposal still expects a decision — which is the
// question the unique index over (tenant, subject, digest) asks, and the reason a
// screen that submits twice gets the same proposal back.
func (p *Proposal) Open() bool { return p.State == StateProposed || p.State == StateApproved }

// NewProposal is what a proposer puts forward: the subject, the change and one
// line about it. It carries no proposer and no base revision, and a request body
// that sends either is refused by the door rather than silently dropped — see
// Proposer on the entity.
//
// SubjectID is present and nil for an entity with one row per tenant: the field is
// required and its value is nothing, because a body that could leave it out and a
// body that could name somebody else's row are the same field with two meanings.
type NewProposal struct {
	SubjectModule string    `json:"subjectModule" required:"true" example:"site"`
	SubjectEntity string    `json:"subjectEntity" required:"true" example:"settings"`
	SubjectID     uuid.UUID `json:"subjectId" format:"uuid" doc:"The row; the nil uuid for an entity with one per tenant"`
	Diff          Diff      `json:"diff" required:"true"`
	Summary       string    `json:"summary" required:"true" maxLength:"200"`
}

// Review is a decision, and only a decision: no body carries a reviewer.
type Review struct {
	Verdict          string `json:"verdict" required:"true" enum:"approved,declined"`
	Comment          string `json:"comment,omitempty" maxLength:"2000"`
	ExpectedRevision int64  `json:"expectedRevision" required:"true" minimum:"1"`
}

// Query narrows the list. State and the subject triple are the two questions a
// screen asks — what is waiting for me, and what is pending on this row.
type Query struct {
	State         string
	SubjectModule string
	SubjectEntity string
	SubjectID     uuid.UUID
	Limit, Offset int
}

// Service is change control: two reads and four commands.
//
// Every command takes the caller's transaction and opens none. Here that rule is
// not a style preference: the proposal's new state, the subject's row and the
// outbox event are one decision, and a subject written while the row that
// authorised it rolled back is a change nobody approved.
//
// Every command rechecks the actor, the tenant scope and the expected revision
// inside that transaction, after locking the row it is about to change. A refusal
// writes nothing, publishes nothing and returns no row.
type Service interface {
	// Propose records a change against the subject's exact current revision and
	// publishes change.proposal_proposed. The same subject and the same digest
	// while a proposal is already open is that proposal *to the account that put it
	// forward*: the row comes back unchanged and nothing is published, because a
	// screen that submits twice is one opinion twice told. The same bytes from a
	// different account are a conflict with no row in it — their retry is not a
	// licence to read somebody else's proposal, which is change:read's to give.
	Propose(ctx context.Context, tx db.Tx[db.Tenant], in NewProposal) (*Proposal, error)

	// Get reads one proposal of this tenant.
	Get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*Proposal, error)

	// List reads a page of them, newest first.
	List(ctx context.Context, tx db.Tx[db.Tenant], q Query) ([]*Proposal, int64, error)

	// Review decides. The verdict must come from an account that is not the
	// proposer's, at the row's exact revision; the same verdict from the same
	// reviewer again changes nothing and says nothing, and a different one is a
	// conflict, because the verdict is on the record.
	Review(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in Review) (*Proposal, error)

	// Apply writes the change. It refuses unless the actor is not the proposer, the
	// row is approved, the subject is still on the revision the diff was made
	// against, and the merge applies; then it writes the subject, stamps the
	// revision it produced, and publishes change.proposal_applied — once, so a
	// retry of an applied proposal by an account that may apply is the same row and
	// no second write. The actor rule is asked first, so the proposer is refused
	// even where another account has already finished the apply.
	Apply(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*Proposal, error)

	// Withdraw takes a proposal back. Only the proposer may, and only while it is
	// open; a decision that has been made is not something the proposer un-says.
	// Who may is decided before what state the row is in, so a second account
	// retrying a finished withdraw is refused the row rather than handed it.
	Withdraw(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*Proposal, error)
}

// Subject is a row a proposal is about, as the module that owns it describes it.
//
// modules/change never opens a table it does not own, so the write an approved
// proposal causes happens in the owner's hands. The two methods are the whole
// contract, and Lock is the half that makes Apply safe: whoever holds the row's
// FOR UPDATE lock decides what its next revision is.
type Subject interface {
	// Lock reads the subject's current JSON and its revision, having locked its row
	// FOR UPDATE in tx.
	Lock(ctx context.Context, tx db.Tx[db.Tenant]) (current json.RawMessage, revision int64, err error)

	// Save writes the merged JSON and returns the subject's new revision.
	Save(ctx context.Context, tx db.Tx[db.Tenant], merged json.RawMessage) (revision int64, err error)
}

// SubjectBinding is one subject this composition can apply. The list of them is
// written out by one author in the application's composition file, its element
// types are checked by the compiler, and a subject missing from it does not
// exist: no init(), no registry, no discovery.
type SubjectBinding struct {
	Module string
	Entity string

	// Resolve builds the subject for one command, over the caller's transaction and
	// the row the proposal names. It returns an error the caller can act on rather
	// than panicking, because the subject may be missing for a reason the request
	// should be told about.
	Resolve func(ctx context.Context, tx db.Tx[db.Tenant], subjectID uuid.UUID) (Subject, error)
}

// Refusal is the answer a consumer gives a write change control requires: the
// door the caller has to go through, in the terms a client can follow.
//
// It is here rather than in the module that refuses because this module owns the
// proposal door every refusal points at, and the shape is the kernel's: a refusal
// that does not name the way through it is not an answer.
type Refusal struct {
	SubjectModule string    `json:"subjectModule"`
	SubjectEntity string    `json:"subjectEntity"`
	SubjectID     uuid.UUID `json:"subjectId"`
	Path          string    `json:"path"`
	Permission    string    `json:"permission"`
}

func (r *Refusal) Error() string {
	return fmt.Sprintf("change: this write is proposed at %s first, which needs %s", r.Path, r.Permission)
}
