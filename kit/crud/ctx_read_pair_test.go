package crud_test

// The three read doors that take a context send the same statements on the same
// handle the older three use, plus one question asked of the caller first. The pair is
// that the answer is identical while the caller is alive — row for row, sentinel for
// sentinel, tenant for tenant — and that the only difference is the ended caller.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// seed writes n tasks for acme and returns their ids in insertion order.
func seed(t *testing.T, conn *db.Conn, n int) []uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, n)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for i := range n {
			task := &Task{Title: string(rune('a'+i)) + " seeded task"}
			if err := crud.Create(ctx, tx, task); err != nil {
				t.Fatalf("Create: %v", err)
			}
			ids = append(ids, task.ID)
		}
	})
	return ids
}

// TestListCtxRefusesAFieldTheSchemaDoesNotKnowTheWayListDoes: a query naming a field of
// no schema is the caller's bug, answered whatever the caller's own state — an argument
// refusal keeps a wiring mistake from arriving as a cancellation to reproduce.
func TestListCtxRefusesAFieldTheSchemaDoesNotKnowTheWayListDoes(t *testing.T) {
	ended, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("the caller walked away")
	cancel(cause)

	for _, q := range []crud.Query{{Sort: "not_a_field"}, {Filter: map[string]any{"not_a_field": "x"}}} {
		_, _, legacy := crud.List[*Task](db.Tx[db.Tenant]{}, q)
		_, _, refused := crud.ListCtx[*Task](ended, db.Tx[db.Tenant]{}, q)
		if !errors.Is(legacy, crud.ErrInvalid) {
			t.Fatalf("List accepted %v; the fixture assumes it refuses", q)
		}
		if !errors.Is(refused, crud.ErrInvalid) {
			t.Errorf("ListCtx answered a bad query with %v, want the ErrInvalid List answers", refused)
		}
		if refused.Error() != legacy.Error() {
			t.Errorf("List says %q and ListCtx says %q", legacy, refused)
		}
		if errors.Is(refused, cause) {
			t.Errorf("an unanswerable query was answered as the caller's ending: %v", refused)
		}
	}
}

// TestGetCtxReadsWhatGetReads: the same row field for field, the same refusal for a
// row that is not there and for another tenant's.
func TestGetCtxReadsWhatGetReads(t *testing.T) {
	conn := setup(t)
	var mine, theirs uuid.UUID
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		task := &Task{Title: "shared reading", Status: "done", Priority: 3}
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatal(err)
		}
		mine = task.ID
	})
	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		task := &Task{Title: "another tenant's"}
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatal(err)
		}
		theirs = task.ID
	})

	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		legacy, errLegacy := crud.Get[*Task](tx, mine)
		got, err := crud.GetCtx[*Task](ctx, tx, mine)
		if err != nil || errLegacy != nil {
			t.Fatalf("Get = %v, GetCtx = %v", errLegacy, err)
		}
		if got.ID != legacy.ID || got.TenantID != legacy.TenantID || got.Title != legacy.Title ||
			got.Status != legacy.Status || got.Priority != legacy.Priority || got.Done != legacy.Done ||
			got.Notes != legacy.Notes || !got.CreatedAt.Equal(legacy.CreatedAt) || !got.UpdatedAt.Equal(legacy.UpdatedAt) {
			t.Errorf("GetCtx read %+v where Get read %+v", got, legacy)
		}
		for name, read := range map[string]func() error{
			"Get":         func() error { _, err := crud.Get[*Task](tx, theirs); return err },
			"GetCtx":      func() error { _, err := crud.GetCtx[*Task](ctx, tx, theirs); return err },
			"Get missing": func() error { _, err := crud.Get[*Task](tx, uuid.New()); return err },
			"GetCtx missing": func() error {
				_, err := crud.GetCtx[*Task](ctx, tx, uuid.New())
				return err
			},
		} {
			if err := read(); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("%s answered %v for a row this tenant may not read, want ErrNotFound", name, err)
			}
		}
	})
}

// TestCountCtxCountsWhatCountCounts: the total is the table's and not the page's.
func TestCountCtxCountsWhatCountCounts(t *testing.T) {
	conn := setup(t)
	ids := seed(t, conn, 4)
	if len(ids) != 4 {
		t.Fatalf("seeded %d rows", len(ids))
	}
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		legacy, errLegacy := crud.Count[*Task](tx)
		got, err := crud.CountCtx[*Task](ctx, tx)
		if err != nil || errLegacy != nil {
			t.Fatalf("Count = %v, CountCtx = %v", errLegacy, err)
		}
		if got != legacy || got != 4 {
			t.Errorf("Count answered %d and CountCtx %d, want 4 each", legacy, got)
		}
	})
	as(t, conn, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		got, err := crud.CountCtx[*Task](ctx, tx)
		if err != nil || got != 0 {
			t.Errorf("another tenant's CountCtx = %d, %v, want 0 rows and no error", got, err)
		}
	})
}

// TestListCtxReturnsWhatListReturns: same page, same order, same total, same clamp.
func TestListCtxReturnsWhatListReturns(t *testing.T) {
	conn := setup(t)
	seed(t, conn, 5)
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		for _, q := range []crud.Query{{Limit: 3}, {Limit: crud.MaxLimit * 3}, {Sort: "-title"}, {Filter: map[string]any{"title": "c seeded task"}}} {
			legacyRows, legacyTotal, errLegacy := crud.List[*Task](tx, q)
			rows, total, err := crud.ListCtx[*Task](ctx, tx, q)
			if err != nil || errLegacy != nil {
				t.Fatalf("List = %v, ListCtx = %v", errLegacy, err)
			}
			if total != legacyTotal {
				t.Errorf("%v: totals differ, %d against %d", q, legacyTotal, total)
			}
			if len(rows) != len(legacyRows) {
				t.Fatalf("%v: pages differ in length, %d against %d", q, len(rows), len(legacyRows))
			}
			for i := range rows {
				if rows[i].ID != legacyRows[i].ID {
					t.Errorf("%v: page %d differs: %s against %s", q, i, rows[i].ID, legacyRows[i].ID)
				}
			}
		}
		_, _, badSort := crud.ListCtx[*Task](ctx, tx, crud.Query{Sort: "not_a_field"})
		_, _, legacyBadSort := crud.List[*Task](tx, crud.Query{Sort: "not_a_field"})
		if !errors.Is(badSort, crud.ErrInvalid) || badSort.Error() != legacyBadSort.Error() {
			t.Errorf("a sort naming no field of the schema: ListCtx says %v and List says %v", badSort, legacyBadSort)
		}
	})
}

// TestGetCtxAnswersTheEndedCallerAndSendsNoStatement: the one behavioural difference,
// this call's and not the transaction's — the new door answers the caller's own cause,
// the older door still reads the row, and the transaction keeps answering.
func TestGetCtxAnswersTheEndedCallerAndSendsNoStatement(t *testing.T) {
	conn := setup(t)
	var mine uuid.UUID
	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		task := &Task{Title: "still there"}
		if err := crud.Create(ctx, tx, task); err != nil {
			t.Fatal(err)
		}
		mine = task.ID
	})

	as(t, conn, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) {
		ended, cancel := context.WithCancelCause(ctx)
		cause := errors.New("the client hung up")
		cancel(cause)

		got, err := crud.GetCtx[*Task](ended, tx, mine)
		if !errors.Is(err, cause) {
			t.Fatalf("GetCtx for an ended caller = %v, want an error wrapping its cause", err)
		}
		if got != nil {
			t.Errorf("GetCtx returned %+v for a caller that had ended", got)
		}
		if errors.Is(err, crud.ErrNotFound) || errors.Is(err, crud.ErrInvalid) {
			t.Errorf("the ended caller's refusal was dressed as %v, which is a different answer to a different question", err)
		}
		want := "crud: the caller ended before the statement: " + cause.Error()
		if err.Error() != want {
			t.Errorf("the sentence is %q, want %q", err, want)
		}

		alive, err := crud.GetCtx[*Task](ctx, tx, mine)
		if err != nil || alive.Title != "still there" {
			t.Errorf("the transaction could not answer its next read after one caller ended: %v", err)
		}
		legacy, errLegacy := crud.Get[*Task](tx, mine)
		if errLegacy != nil || legacy.ID != mine {
			t.Errorf("Get refused a row that exists: %v", errLegacy)
		}
	})
}

// TestSoftDeleteDeleteRowAndCountCtxRefuseTheEndedCallerToo: the guard is the write
// doors' and the count's too; the zero handle sends a door that ran for a nil database.
func TestSoftDeleteDeleteRowAndCountCtxRefuseTheEndedCallerToo(t *testing.T) {
	ended, cancel := context.WithCancelCause(context.Background())
	cause := errors.New("the tick ended")
	cancel(cause)
	tx, id := db.Tx[db.Tenant]{}, uuid.New()
	_, counted := crud.CountCtx[*Task](ended, tx)
	asked := map[string]error{
		"SoftDelete": crud.SoftDelete[*Task](ended, tx, id),
		"DeleteRow":  crud.DeleteRow[*Task](ended, tx, id),
		"CountCtx":   counted,
	}
	for door, err := range asked {
		if !errors.Is(err, cause) || err.Error() != "crud: the caller ended before the statement: "+cause.Error() {
			t.Errorf("%s answered %v for an ended caller, want its cause named in the door's own sentence", door, err)
		}
	}
}
