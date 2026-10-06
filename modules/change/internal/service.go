// Package internal is every implementation of the change module. Nothing outside
// modules/change can import it, which is the compiler enforcing idea 3: a consumer
// takes contracts.Service, and taking anything else does not build.
package internal

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

// Service is change control over the tenant's own table, plus the subjects this
// composition allowed it to write.
//
// It holds a service and a literal list of subject bindings, both handed over at
// composition: the list is what makes "which changes can be applied here" a fact
// somebody wrote down rather than something discovered at runtime.
type Service struct {
	subjects map[subjectKey]contracts.SubjectBinding
}

type subjectKey struct{ module, entity string }

// NewService binds the subjects this installation applies proposals for. It
// panics on a binding nobody could resolve or on two bindings for one subject,
// because both are composition mistakes and a request should never be the one to
// find out: the boot failure names the binding, the runtime one would name a
// customer.
func NewService(bindings []contracts.SubjectBinding) *Service {
	s := &Service{subjects: make(map[subjectKey]contracts.SubjectBinding, len(bindings))}
	for _, b := range bindings {
		if b.Module == "" || b.Entity == "" || b.Resolve == nil {
			panic(fmt.Sprintf("change: the subject binding %q/%q is incomplete", b.Module, b.Entity))
		}
		key := subjectKey{b.Module, b.Entity}
		if _, taken := s.subjects[key]; taken {
			panic(fmt.Sprintf("change: %s/%s is bound twice, and only one of them would be used", b.Module, b.Entity))
		}
		s.subjects[key] = b
	}
	return s
}

var _ contracts.Service = (*Service)(nil)

// unwritable names the first key of a diff this subject cannot move, and answers true
// when it can move every one of them.
//
// A subject that does not say (a Subject that is not a contracts.Writable) is asked
// nothing, which is what leaves every binding written before this question standing
// exactly where it stood: the merged document is then its own owner's business, as it
// was. Where the subject does say, the keys are asked in sorted order, so two requests
// carrying the same impossible diff name the same field.
func unwritable(subject contracts.Subject, diff contracts.Diff) (string, bool) {
	w, ok := subject.(contracts.Writable)
	if !ok {
		return "", true
	}
	allowed := w.WritableFields()
	for _, field := range slices.Sorted(maps.Keys(diff)) {
		if !slices.Contains(allowed, field) {
			return field, false
		}
	}
	return "", true
}

// actor is the person the request came from. No command reads one from a body: an
// identity a caller could send is an identity a caller could choose, and the whole
// four-eyes rule is a note on a form otherwise.
func actor(ctx context.Context) (uuid.UUID, error) {
	id, ok := tenancy.ActorFrom(ctx)
	if !ok {
		return uuid.Nil, fmt.Errorf("%w: change control needs to know who is asking", crud.ErrInvalid)
	}
	return id, nil
}

// Propose records a change against the subject's exact current revision. See
// contracts.Service.
func (s *Service) Propose(ctx context.Context, tx db.Tx[db.Tenant], in contracts.NewProposal) (*contracts.Proposal, error) {
	proposer, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	binding, ok := s.binding(in.SubjectModule, in.SubjectEntity)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s: %w", crud.ErrConflict, in.SubjectModule, in.SubjectEntity,
			contracts.ErrUnsupportedSubject)
	}
	digest, err := in.Diff.Digest()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrInvalid, err)
	}
	if existing, err := s.openDuplicate(tx, in.SubjectModule, in.SubjectEntity, in.SubjectID, digest); err != nil {
		return nil, err
	} else if existing != nil {
		return s.oneOpinion(proposer, existing)
	}
	// The subject is locked before its revision is read, so the base revision this
	// row records is the revision no other transaction can move out from under it
	// while this one is open. It also answers whether the diff can apply at all:
	// a proposal nobody could ever apply is refused here rather than at the review.
	subject, err := binding.Resolve(ctx, tx, in.SubjectID)
	if err != nil {
		return nil, err
	}
	current, revision, err := subject.Lock(ctx, tx)
	if err != nil {
		return nil, err
	}
	// A diff is only a promise if some apply could keep it. The subject is the one
	// that knows which of its fields a Save writes, and a name outside that list
	// merges into nothing: the proposal would be digested, decided and applied, the
	// row would not move as reviewed, and the state would say it had.
	if field, ok := unwritable(subject, in.Diff); !ok {
		return nil, fmt.Errorf("%w: %s/%s has no field %q a proposal can move: it is either not a field of the subject, or one a command owns outright",
			crud.ErrInvalid, in.SubjectModule, in.SubjectEntity, field)
	}
	if _, err := in.Diff.Merge(current); err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrInvalid, err)
	}
	at := db.Now()
	row := &contracts.Proposal{
		SubjectModule: in.SubjectModule,
		SubjectEntity: in.SubjectEntity,
		SubjectID:     in.SubjectID,
		BaseRevision:  revision,
		Diff:          in.Diff,
		DiffDigest:    digest,
		Summary:       in.Summary,
		Proposer:      proposer,
		State:         contracts.StateProposed,
		Revision:      1,
	}
	if err := s.insert(ctx, tx, row); err != nil {
		// The unique index over an open proposal is the answer to the race the
		// duplicate check above cannot see: two inserts at once. Postgres reports the
		// loser only after the winner has committed, so the row is visible now, and the
		// loser answers with it the way the sequential path does — through oneOpinion,
		// which decides whether that is the row or the refusal. A caller who clicked
		// twice is told which proposal exists rather than that something conflicted,
		// because "conflict, no id" does not say whether the change is queued at all.
		if existing, dupErr := s.openDuplicate(tx, in.SubjectModule, in.SubjectEntity, in.SubjectID, digest); dupErr != nil {
			return nil, dupErr
		} else if existing != nil {
			return s.oneOpinion(proposer, existing)
		}
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventProposed, contracts.Proposed{
		ProposalID: row.ID, SubjectModule: row.SubjectModule, SubjectEntity: row.SubjectEntity,
		SubjectID: row.SubjectID, BaseRevision: row.BaseRevision, DiffDigest: row.DiffDigest,
		Diff: row.Diff, Summary: row.Summary, Proposer: row.Proposer, At: at,
	})
}

// insert writes the new row inside a savepoint, which is what makes Propose's
// collision recoverable at all.
//
// Postgres answers a conflicting insert with 23505 and puts the *whole*
// transaction into the aborted state, where every later statement is refused
// until it rolls back. So the recovery read Propose does after a lost race —
// and the event the winner of that race still has to publish — cannot run in the
// transaction the failed insert left behind: the caller gets the insert error and
// no row, which is Finding "the second submit of the same diff gets a conflict and
// no proposal" stated as a mechanism. The savepoint is the half-rollback that
// keeps the transaction usable: on the way out it is either still open and about
// to commit, or the insert is unwound and the transaction answers again.
//
// The name is a literal and the savepoint is never released: Postgres discards
// what is still open at commit, and a second propose in one request transaction
// nests a savepoint of the same name, which ROLLBACK TO resolves to the inner one.
func (s *Service) insert(ctx context.Context, tx db.Tx[db.Tenant], row *contracts.Proposal) error {
	if err := tx.DB().SavePoint("change_propose").Error; err != nil {
		return err
	}
	created := crud.Create(ctx, tx, row)
	if created == nil {
		return nil
	}
	if back := tx.DB().RollbackTo("change_propose").Error; back != nil {
		// The insert failed and the unwind failed with it; the transaction is aborted
		// and nothing above it can commit, so the unwind's error is the one worth
		// naming and the insert's is the reason it happened.
		return fmt.Errorf("%w (the failed insert: %v)", back, created)
	}
	return created
}

// Get reads one proposal of this tenant. See contracts.Service.
func (s *Service) Get(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.Proposal, error) {
	return crud.Get[*contracts.Proposal](tx, id)
}

// List reads a page of them. See contracts.Service.
func (s *Service) List(_ context.Context, tx db.Tx[db.Tenant], q contracts.Query) ([]*contracts.Proposal, int64, error) {
	// The names kit/crud's filters are written in are the entity's JSON names, the
	// same ones a query parameter carries, so one field has one spelling in every
	// door that reads it.
	filter := map[string]any{}
	for field, value := range map[string]string{
		"state": q.State, "subjectModule": q.SubjectModule, "subjectEntity": q.SubjectEntity,
	} {
		if value != "" {
			filter[field] = value
		}
	}
	if q.SubjectID != uuid.Nil {
		filter["subjectId"] = q.SubjectID
	}
	return crud.List[*contracts.Proposal](tx, crud.Query{
		Limit: q.Limit, Offset: q.Offset, Filter: filter,
	})
}

// Review decides a proposal. See contracts.Service.
func (s *Service) Review(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in contracts.Review) (*contracts.Proposal, error) {
	reviewer, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	row, err := s.open(ctx, tx, id, in.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	verdict := in.Verdict
	if verdict != contracts.VerdictApproved && verdict != contracts.VerdictDeclined {
		return nil, fmt.Errorf("%w: a verdict is approved or declined, not %q", crud.ErrInvalid, verdict)
	}
	if reviewer == row.Proposer {
		// Asked before the row's state, so the author cannot read a decided proposal
		// back through a retry of the verdict somebody else wrote. Wrapped in crud's
		// conflict as well as in its own sentinel, so the one error mapping in kit/rest
		// answers with the status the kernel uses for "the row is not in a state that
		// allows this" and the client still gets the sentence that names the cure.
		// errors.Is reaches either half.
		return nil, fmt.Errorf("%w: %w", crud.ErrConflict, contracts.ErrSelfReview)
	}
	if row.State != contracts.StateProposed {
		// A verdict is one-way: review is legal only from proposed. Open() is the wrong
		// question — an approved row still expects its apply but holds its verdict: the
		// reviewer's own same verdict again, otherwise a conflict, and the row stands.
		if row.Verdict == verdict && row.Reviewer != nil && *row.Reviewer == reviewer {
			return row, nil
		}
		return nil, fmt.Errorf("%w: this proposal was already %s", crud.ErrConflict, row.State)
	}
	at := db.Now()
	row.State = contracts.StateDeclined
	if verdict == contracts.VerdictApproved {
		row.State = contracts.StateApproved
	}
	row.Verdict, row.Comment, row.Reviewer, row.ReviewedAt = verdict, in.Comment, &reviewer, &at
	row.Revision++
	if err := crud.Update(ctx, tx, row, "state", "verdict", "comment", "reviewer", "reviewed_at", "revision", "updated_at"); err != nil {
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventReviewed, contracts.Reviewed{
		ProposalID: row.ID, Verdict: verdict, Reviewer: reviewer, Proposer: row.Proposer,
		Comment: row.Comment, State: row.State, At: at,
	})
}

// Apply writes the change the row was approved for. See contracts.Service.
func (s *Service) Apply(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*contracts.Proposal, error) {
	applier, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	row, err := s.open(ctx, tx, id, expectedRevision)
	if err != nil {
		return nil, err
	}
	if applier == row.Proposer {
		// The author is refused whether or not the change is already written: the rule
		// is about who may apply, not about whether an apply is still owed. Answering a
		// proposer's retry of somebody else's apply with the applied row would hand the
		// author the verdict through a command they are refused on every other path.
		return nil, fmt.Errorf("%w: %w", crud.ErrConflict, contracts.ErrSelfReview)
	}
	if row.State == contracts.StateApplied {
		// Applied once, and a retry by an account that may apply sees the same row. The
		// subject is not written again, because the thing that was approved was one
		// write, not however many times the client managed to ask for it.
		return row, nil
	}
	if row.State != contracts.StateApproved {
		return nil, fmt.Errorf("%w: only an approved proposal may be applied, and this one is %s", crud.ErrConflict, row.State)
	}
	binding, ok := s.binding(row.SubjectModule, row.SubjectEntity)
	if !ok {
		return nil, fmt.Errorf("%w: %s/%s: %w", crud.ErrConflict, row.SubjectModule, row.SubjectEntity,
			contracts.ErrUnsupportedSubject)
	}
	subject, err := binding.Resolve(ctx, tx, row.SubjectID)
	if err != nil {
		return nil, err
	}
	// The lock, then the staleness question. Asked the other way round, the answer
	// would be about a revision that had already moved by the time it was read.
	current, revision, err := subject.Lock(ctx, tx)
	if err != nil {
		return nil, err
	}
	if revision != row.BaseRevision {
		// Staleness is derived here rather than stored on the row: any write to the
		// subject would make a stored "not stale" wrong before anybody read it, and a
		// fact that goes wrong on its own is not evidence. Nothing is written and
		// nothing is published; the proposal stays approved, for whoever re-diffs it.
		return nil, fmt.Errorf("%w: it was made against revision %d and the subject is on %d: %w",
			crud.ErrConflict, row.BaseRevision, revision, contracts.ErrStaleBase)
	}
	merged, err := row.Diff.Merge(current)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", crud.ErrConflict, err)
	}
	newRevision, err := subject.Save(ctx, tx, merged)
	if err != nil {
		return nil, err
	}
	at := db.Now()
	row.State = contracts.StateApplied
	row.AppliedRevision = newRevision
	row.Revision++
	if err := crud.Update(ctx, tx, row, "state", "applied_revision", "revision", "updated_at"); err != nil {
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventApplied, contracts.Applied{
		ProposalID: row.ID, SubjectModule: row.SubjectModule, SubjectEntity: row.SubjectEntity,
		SubjectID: row.SubjectID, Reviewer: deref(row.Reviewer), Proposer: row.Proposer,
		BaseRevision: row.BaseRevision, NewRevision: newRevision, DiffDigest: row.DiffDigest, At: at,
	})
}

// Withdraw takes a proposal back. See contracts.Service.
func (s *Service) Withdraw(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*contracts.Proposal, error) {
	proposer, err := actor(ctx)
	if err != nil {
		return nil, err
	}
	row, err := s.open(ctx, tx, id, expectedRevision)
	if err != nil {
		return nil, err
	}
	if proposer != row.Proposer {
		// Only the person who put it forward may take it back; a decider who disagrees
		// declines it, which leaves both opinions on the record. This is asked before the
		// row's state, so a second account retrying an author's withdraw is refused the
		// withdrawn row rather than handed it: the idempotent answer belongs to whoever
		// the command would have accepted the first time.
		return nil, fmt.Errorf("%w: only the proposer may withdraw a proposal", crud.ErrConflict)
	}
	if row.State == contracts.StateWithdrawn {
		return row, nil
	}
	if !row.Open() {
		return nil, fmt.Errorf("%w: a %s proposal is over", crud.ErrConflict, row.State)
	}
	at := db.Now()
	from := row.State
	row.State = contracts.StateWithdrawn
	row.Revision++
	if err := crud.Update(ctx, tx, row, "state", "revision", "updated_at"); err != nil {
		return nil, err
	}
	return row, events.Publish(ctx, tx, contracts.EventWithdrawn, contracts.Withdrawn{
		ProposalID: row.ID, Proposer: row.Proposer, FromState: from, At: at,
	})
}

// oneOpinion answers the submitter whose diff is already sitting in the queue.
//
// From the author who put it there it is one proposal and the row as it stands, with
// nothing published: a screen that submits twice must not appear in the queue twice
// and must not reach the trail twice. From anybody else it is a refusal that names no
// row. The partial unique index over one open proposal per (subject, diff) means
// their bytes cannot have a second row, and handing them the first author's summary,
// proposer and later verdict would be a read of a proposal they hold no `change:read`
// grant for — the queue's answer to them is that the change is already in it.
func (s *Service) oneOpinion(proposer uuid.UUID, existing *contracts.Proposal) (*contracts.Proposal, error) {
	if existing.Proposer == proposer {
		return existing, nil
	}
	return nil, fmt.Errorf("%w: an identical change to %s/%s %s is already proposed by somebody else",
		crud.ErrConflict, existing.SubjectModule, existing.SubjectEntity, existing.SubjectID)
}

// open locks the row and rechecks the two things a mutation may not be decided
// without: that the row is this tenant's — row-level security narrowed the locked
// read to it, and crud.Update, which each mutation here finishes with, says so out
// loud again (its call to RecheckTenant) — and that it is still the revision the
// caller saw. The lock is what makes that second check mean anything: without it
// the row could move between the read and the write, and two reviewers both get yes.
func (s *Service) open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expectedRevision int64) (*contracts.Proposal, error) {
	row, err := crud.GetForUpdate[*contracts.Proposal](tx, id)
	if err != nil {
		return nil, err
	}
	if expectedRevision != row.Revision {
		// The caller decided about a revision it had read. Anything else is two
		// deciders who both clicked, and only one of them can be right.
		return nil, fmt.Errorf("%w: this proposal is at revision %d", crud.ErrConflict, row.Revision)
	}
	return row, nil
}

// binding is the subject this composition can apply, if it can apply this one.
func (s *Service) binding(module, entity string) (contracts.SubjectBinding, bool) {
	b, ok := s.subjects[subjectKey{module, entity}]
	return b, ok
}

// openDuplicate is the same subject and the same bytes, still open, or nil.
func (s *Service) openDuplicate(tx db.Tx[db.Tenant], module, entity string, id uuid.UUID, digest string) (*contracts.Proposal, error) {
	var row contracts.Proposal
	err := tx.DB().Model(contracts.Proposal{}).
		Where("deleted_at IS NULL AND state IN ? AND subject_module = ? AND subject_entity = ? AND subject_id = ? AND diff_digest = ?",
			[]string{contracts.StateProposed, contracts.StateApproved}, module, entity, id, digest).
		Take(&row).Error
	switch classified := crud.Classify(err); {
	case classified == nil:
		return &row, nil
	case errors.Is(classified, crud.ErrNotFound):
		return nil, nil
	default:
		return nil, classified
	}
}

func deref(id *uuid.UUID) uuid.UUID {
	if id == nil {
		return uuid.Nil
	}
	return *id
}
