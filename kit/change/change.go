// Package change is the kernel's change control: a change to any subject is proposed against an exact
// revision of it, reviewed by an account that is not the proposer, and only then applied — and applied only
// while the subject is still at the revision that was reviewed.
//
// It is the generic form of a pattern modules kept writing for themselves (services-law's fee review is the
// one that shipped, and the one that moves onto this first). A module keeps its own entity and its own idea of
// a revision; what it hands this package is the subject's kind and id, the revision the proposal was made
// against, and the change itself as JSON this package stores and never reads.
//
// Every command runs in the caller's tenant transaction and acts as the request's principal: the proposer,
// the reviewer and the applier are never parameters, so no caller can record a review by somebody else. Every
// transition publishes an event (change.proposed, change.reviewed, change.withdrawn, change.applied), which
// modules/audit records with the rest of the trail, and the row keeps its own who and when.
package change

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The states a proposal moves through, and the events each transition publishes.
const (
	StatePending   = "pending"
	StateApproved  = "approved"
	StateRejected  = "rejected"
	StateWithdrawn = "withdrawn"
	StateApplied   = "applied"

	EventProposed  = "change.proposed"
	EventReviewed  = "change.reviewed"
	EventWithdrawn = "change.withdrawn"
	EventApplied   = "change.applied"
)

// Events is every event this package publishes, for a composition that declares them.
var Events = []string{EventProposed, EventReviewed, EventWithdrawn, EventApplied}

var (
	// ErrNoPrincipal: change control records who did what, so a request nobody signed in to cannot act.
	ErrNoPrincipal = errors.New("change: this needs a signed-in account")
	// ErrSelfReview: the proposer cannot review their own proposal, whatever their roles.
	ErrSelfReview = errors.New("change: the proposer cannot review their own proposal")
	// ErrNotPending: a proposal is reviewed or withdrawn once.
	ErrNotPending = errors.New("change: the proposal is no longer pending")
	// ErrNotApproved: only an approved proposal is applied.
	ErrNotApproved = errors.New("change: the proposal has not been approved")
	// ErrStale: the subject has moved since the proposal was made against it, so what was reviewed is not
	// what would be changed. A new proposal against the current revision is the way forward.
	ErrStale = errors.New("change: the subject is no longer at the revision that was reviewed")
	// ErrNotFound: no proposal with that id in this tenant.
	ErrNotFound = errors.New("change: no such proposal")
)

// Proposal is what a module asks to change.
type Proposal struct {
	SubjectKind string // the entity's kind, as its module names it: "fee", "setting", ...
	SubjectID   string
	Revision    string // the subject's revision the proposal is made against
	Diff        any    // the change, encoded as JSON; this package never reads it
	Summary     string // one line a reviewer reads first
}

// Change is a proposal as the table holds it.
type Change struct {
	ID          uuid.UUID
	SubjectKind string
	SubjectID   string
	Revision    string
	Diff        json.RawMessage
	Summary     string
	State       string
	ProposedBy  uuid.UUID
	ReviewedBy  *uuid.UUID
	ReviewedAt  *time.Time
	AppliedAt   *time.Time
	Reason      string
	CreatedAt   time.Time
}

// Event is the payload of every transition: ids and the state reached, never the diff, because an event is
// copied into the audit trail and kept, and the change's content belongs to its subject's module.
type Event struct {
	ProposalID  uuid.UUID `json:"proposalId"`
	SubjectKind string    `json:"subjectKind"`
	SubjectID   string    `json:"subjectId"`
	Revision    string    `json:"revision"`
	State       string    `json:"state"`
	Actor       uuid.UUID `json:"actorId"`
	Reason      string    `json:"reason,omitempty"`
	At          time.Time `json:"at"`
}

func actor(ctx context.Context) (uuid.UUID, error) {
	p, ok := tenancy.PrincipalFrom(ctx)
	if !ok || p.UserID == uuid.Nil {
		return uuid.Nil, ErrNoPrincipal
	}
	return p.UserID, nil
}

// Propose records a proposal, pending review, as the request's principal.
func Propose(ctx context.Context, tx db.Tx[db.Tenant], p Proposal) (*Change, error) {
	who, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	if p.SubjectKind == "" || p.SubjectID == "" || p.Revision == "" {
		return nil, errors.New("change: a proposal names its subject's kind, id and revision")
	}
	diff, err := json.Marshal(p.Diff)
	if err != nil {
		return nil, fmt.Errorf("change: encode the proposal: %w", err)
	}
	var id uuid.UUID
	err = tx.DB().Raw(`INSERT INTO change_proposals (tenant_id, subject_kind, subject_id, revision, diff, summary, proposed_by)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		db.TenantOf(tx).ID, p.SubjectKind, p.SubjectID, p.Revision, string(diff), p.Summary, who).Row().Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("change: record the proposal: %w", err)
	}
	c, err := Get(tx, id)
	if err != nil {
		return nil, err
	}
	return c, publish(ctx, tx, EventProposed, c, who, "")
}

// Review approves or rejects a pending proposal, as the request's principal, who must not be its proposer.
func Review(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, approve bool, reason string) (*Change, error) {
	who, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := locked(tx, id)
	if err != nil {
		return nil, err
	}
	if c.ProposedBy == who {
		return nil, ErrSelfReview
	}
	if c.State != StatePending {
		return nil, ErrNotPending
	}
	state := StateRejected
	if approve {
		state = StateApproved
	}
	if err := tx.DB().Exec(`UPDATE change_proposals SET state = ?, reviewed_by = ?, reviewed_at = now(), reason = ?, updated_at = now() WHERE id = ?`,
		state, who, reason, id).Error; err != nil {
		return nil, fmt.Errorf("change: record the review: %w", err)
	}
	if c, err = Get(tx, id); err != nil {
		return nil, err
	}
	return c, publish(ctx, tx, EventReviewed, c, who, reason)
}

// Withdraw takes a pending proposal back; only its proposer may.
func Withdraw(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, reason string) (*Change, error) {
	who, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := locked(tx, id)
	if err != nil {
		return nil, err
	}
	if c.ProposedBy != who {
		return nil, ErrNotFound // somebody else's proposal is not theirs to see withdrawn
	}
	if c.State != StatePending {
		return nil, ErrNotPending
	}
	if err := tx.DB().Exec(`UPDATE change_proposals SET state = ?, reason = ?, updated_at = now() WHERE id = ?`,
		StateWithdrawn, reason, id).Error; err != nil {
		return nil, fmt.Errorf("change: record the withdrawal: %w", err)
	}
	if c, err = Get(tx, id); err != nil {
		return nil, err
	}
	return c, publish(ctx, tx, EventWithdrawn, c, who, reason)
}

// Apply carries out an approved proposal, in the caller's transaction, while the subject is still at the
// revision that was reviewed. current is the subject's revision now, read by its owner in this transaction;
// apply is the owner's own write of the change, handed the stored diff. Either both commit or neither does.
func Apply(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, current string, apply func(diff json.RawMessage) error) (*Change, error) {
	who, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	c, err := locked(tx, id)
	if err != nil {
		return nil, err
	}
	if c.State != StateApproved {
		return nil, ErrNotApproved
	}
	if current != c.Revision {
		return nil, ErrStale
	}
	if err := apply(c.Diff); err != nil {
		return nil, err
	}
	if err := tx.DB().Exec(`UPDATE change_proposals SET state = ?, applied_at = now(), updated_at = now() WHERE id = ?`,
		StateApplied, id).Error; err != nil {
		return nil, fmt.Errorf("change: record the application: %w", err)
	}
	if c, err = Get(tx, id); err != nil {
		return nil, err
	}
	return c, publish(ctx, tx, EventApplied, c, who, "")
}

// Get reads one proposal in the transaction's tenant.
func Get(tx db.Tx[db.Tenant], id uuid.UUID) (*Change, error) {
	return read(tx, id, "")
}

// locked reads one proposal and holds its row until the transaction ends, so two reviews or a review and an
// application cannot interleave.
func locked(tx db.Tx[db.Tenant], id uuid.UUID) (*Change, error) {
	return read(tx, id, " FOR UPDATE")
}

func read(tx db.Tx[db.Tenant], id uuid.UUID, lock string) (*Change, error) {
	var c Change
	var diff string
	row := tx.DB().Raw(`SELECT id, subject_kind, subject_id, revision, diff::text, summary, state, proposed_by,
		reviewed_by, reviewed_at, applied_at, reason, created_at FROM change_proposals WHERE id = ?`+lock, id).Row()
	err := row.Scan(&c.ID, &c.SubjectKind, &c.SubjectID, &c.Revision, &diff, &c.Summary, &c.State, &c.ProposedBy,
		&c.ReviewedBy, &c.ReviewedAt, &c.AppliedAt, &c.Reason, &c.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("change: read proposal %s: %w", id, err)
	}
	c.Diff = json.RawMessage(diff)
	return &c, nil
}

func publish(ctx context.Context, tx db.Tx[db.Tenant], name string, c *Change, who uuid.UUID, reason string) error {
	return events.Publish(ctx, tx, name, Event{
		ProposalID: c.ID, SubjectKind: c.SubjectKind, SubjectID: c.SubjectID, Revision: c.Revision,
		State: c.State, Actor: who, Reason: reason, At: db.Now(),
	})
}
