package crud_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/fault"
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

	// A refusal that is already one of the three is not a driver error, and
	// Classify leaves it alone whichever name it arrived under.
	for _, err := range []error{fmt.Errorf("validate: %w", fault.ErrInvalid), fmt.Errorf("validate: %w", crud.ErrInvalid)} {
		if !errors.Is(crud.Classify(err), fault.ErrInvalid) {
			t.Errorf("Classify changed a refusal that was already classified: %v", err)
		}
	}
}
