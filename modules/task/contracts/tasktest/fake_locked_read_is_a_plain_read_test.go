package tasktest_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
)

// contracts.LockedReader is the read a diff is made against, and a consumer that
// tests its apply path against this double needs the double to answer it. What the
// fake can promise is the read; what it cannot is the row lock, which is a
// Postgres fact modules/task/internal proves against the schema. These cases pin
// which of the two this is, so a green consumer suite over this double is never
// read as proof about locking.

var _ contracts.LockedReader = (*tasktest.Fake)(nil)

// TestTheFakeLockedReaderAnswersTheSameRowAsThePlainRead compares the two reads
// on what a read can carry: the same id, the same fields, the same revision.
func TestTheFakeLockedReaderAnswersTheSameRowAsThePlainRead(t *testing.T) {
	ctx := tenancy.WithTenant(t.Context(), acme)
	fake := tasktest.NewFake()
	id := fake.Put(ctx, &contracts.Task{Title: "chiller"})

	locked, err := fake.TaskForUpdate(ctx, db.Tx[db.Tenant]{}, id)
	if err != nil {
		t.Fatalf("TaskForUpdate: %v", err)
	}
	plain, err := fake.TaskByID(ctx, db.Tx[db.Tenant]{}, id)
	if err != nil {
		t.Fatalf("TaskByID: %v", err)
	}
	if rowAnswer(locked) != rowAnswer(plain) {
		t.Errorf("the locked read is %s and the plain read is %s; one row, one answer", rowAnswer(locked), rowAnswer(plain))
	}
}

// TestTheFakeLockedReaderRefusesWhatTheTenantCannotReach takes the two refusals a
// read answers — an id nobody made and one another tenant holds — and checks each
// leaves the store and the witness untouched. Both are immutable: the same id
// asked again answers the same way, and a read may not say more than that.
func TestTheFakeLockedReaderRefusesWhatTheTenantCannotReach(t *testing.T) {
	ctx := tenancy.WithTenant(t.Context(), acme)
	fake := tasktest.NewFake()
	id := fake.Put(ctx, &contracts.Task{Title: "chiller"})
	other := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: "other", Name: "Other"})

	held, rows := fake.Count(ctx), fake.Count(other)
	for _, missing := range []struct {
		which string
		ctx   context.Context
		id    uuid.UUID
	}{
		{"an id nobody made", ctx, uuid.New()},
		{"a row another tenant holds", other, id},
	} {
		got, err := fake.TaskForUpdate(missing.ctx, db.Tx[db.Tenant]{}, missing.id)
		if got != nil || !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("TaskForUpdate of %s answered %v, %v; want nothing and crud.ErrNotFound", missing.which, got, err)
		}
		if fake.Count(ctx) != held || fake.Count(other) != rows {
			t.Errorf("the refused %s wrote a row: acme holds %d (was %d), other holds %d (was %d)",
				missing.which, fake.Count(ctx), held, fake.Count(other), rows)
		}
	}
	if names := fake.Events.Names(); len(names) != 0 {
		t.Errorf("a refused read published %v; a read says nothing", names)
	}
}

// rowAnswer renders the fields both reads of one row must agree on.
func rowAnswer(task *contracts.Task) string {
	if task == nil {
		return "nothing"
	}
	return fmt.Sprintf("id=%s title=%q status=%s revision=%d", task.ID, task.Title, task.Status, task.Revision)
}
