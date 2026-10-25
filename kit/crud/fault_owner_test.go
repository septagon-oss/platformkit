package crud_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestSentinelsAreTheSharedValues pins the move: kit/fault owns the three
// refusals and this package re-exports the very same values, so the value
// package that refuses a write and the storage adapter that classifies one hold
// one opinion, which is what lets one HTTP mapping answer for both. Identity is
// asserted as pointer equality because a re-declared errors.New with the same
// text would satisfy errors.Is in neither direction, fork every message that
// reaches a log or a problem detail, and read as a second answer to what a 404
// means. The messages are asserted against the literals, not against each
// other: they are wire data, and an alias makes them equal for free.
func TestSentinelsAreTheSharedValues(t *testing.T) {
	for _, c := range []struct {
		name     string
		exported error
		owner    error
		message  string
	}{
		{"ErrNotFound", crud.ErrNotFound, fault.ErrNotFound, "crud: no such row"},
		{"ErrInvalid", crud.ErrInvalid, fault.ErrInvalid, "crud: invalid"},
		{"ErrConflict", crud.ErrConflict, fault.ErrConflict, "crud: conflict"},
	} {
		if c.exported != c.owner {
			t.Errorf("%s: kit/crud and kit/fault declare different values (%p and %p); they must be one object",
				c.name, c.exported, c.owner)
		}
		if !errors.Is(fmt.Errorf("write: %w", c.owner), c.exported) {
			t.Errorf("errors.Is(a wrap of fault.%s, crud.%s) = false", c.name, c.name)
		}
		if !errors.Is(fmt.Errorf("write: %w", c.exported), c.owner) {
			t.Errorf("errors.Is(a wrap of crud.%s, fault.%s) = false", c.name, c.name)
		}
		if c.exported.Error() != c.message || c.owner.Error() != c.message {
			t.Errorf("%s message changed: crud package says %q, fault package says %q, wire says %q",
				c.name, c.exported, c.owner, c.message)
		}
	}
}

// TestClassifyMapsToTheSharedSentinels: Classify is the only place a driver
// error becomes one of the three, and kit/rest maps the three to 404, 422 and
// 409. A wrapped driver error has to be matchable with the same values a value
// package wraps, or the mapping depends on which side of the storage adapter
// the caller stood on. The literal message is pinned as diagnostics: it is what
// a log line and a wrapped, non-unique ErrConflict carry. It is deliberately not
// what a duplicate reaches a client with — Fault answers a *UniqueConflict with
// problem.Conflict's own sentence so a constraint name stays off the wire
// (kit/rest/rest.go) — and this assertion must not be read as promising the
// opposite.
func TestClassifyMapsToTheSharedSentinels(t *testing.T) {
	missing := crud.Classify(fmt.Errorf("read: %w", gorm.ErrRecordNotFound))
	if !errors.Is(missing, fault.ErrNotFound) || !errors.Is(missing, crud.ErrNotFound) {
		t.Errorf("a wrapped missing row classified as %v", missing)
	}

	duplicate := crud.Classify(&pgconn.PgError{Code: "23505", ConstraintName: "internal_title_key"})
	if !errors.Is(duplicate, fault.ErrConflict) || !errors.Is(duplicate, crud.ErrConflict) {
		t.Errorf("a unique violation classified as %v", duplicate)
	}
	if _, unique := errors.AsType[*crud.UniqueConflict](duplicate); !unique {
		t.Errorf("a unique violation lost its typed kind: %v", duplicate)
	}
	if duplicate.Error() != "crud: conflict: internal_title_key" {
		t.Errorf("diagnostics = %q", duplicate)
	}

	// A refusal already one of the three is no driver error, and Classify leaves
	// it alone whichever name it arrived under.
	for _, err := range []error{fmt.Errorf("validate: %w", fault.ErrInvalid), fmt.Errorf("validate: %w", crud.ErrInvalid)} {
		if !errors.Is(crud.Classify(err), fault.ErrInvalid) {
			t.Errorf("Classify changed a refusal that was already classified: %v", err)
		}
	}
}

// TestTheInMemoryStoreAndTheReadDoorAnswerOneNotFoundForNoRow: the double a command
// suite runs on and the SQL door have to refuse a missing row with one object, or a
// command passes on the fake and answers another status at the database. GetCtx
// classifies a driver's not-found in one place, Classify, so the two halves meet
// there with no database in the way. The soft/hard split and the deleted_at stamp
// stay out, because an in-memory store holds neither: those are proved at the schema.
func TestTheInMemoryStoreAndTheReadDoorAnswerOneNotFoundForNoRow(t *testing.T) {
	store := porttest.NewStore(func(row *Task) uuid.UUID { return row.ID })
	ctx := tenancy.WithTenant(context.Background(), acme)
	_, fromTheStore := store.Get(ctx, uuid.New())
	if !errors.Is(fromTheStore, crud.ErrNotFound) || !errors.Is(fromTheStore, fault.ErrNotFound) {
		t.Errorf("the store refused a row it does not hold with %v, want the shared ErrNotFound", fromTheStore)
	}
	fromTheDoor := crud.Classify(fmt.Errorf("read: %w", gorm.ErrRecordNotFound))
	if !errors.Is(fromTheDoor, crud.ErrNotFound) || !errors.Is(fromTheDoor, fault.ErrNotFound) {
		t.Errorf("the read door classified a missing row as %v, want the shared ErrNotFound", fromTheDoor)
	}
	if crud.ErrNotFound != fault.ErrNotFound {
		t.Errorf("crud.ErrNotFound and fault.ErrNotFound are %p and %p; a fake and a service holding different values answer a 404 twice",
			crud.ErrNotFound, fault.ErrNotFound)
	}
}
