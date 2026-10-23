package task_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"github.com/septagon-oss/platformkit/modules/task/internal"
)

func TestCreatorCarriesViolationsLikeItsFake(t *testing.T) {
	admin, conn := dbtest.Schema(t, task.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	in := contracts.CreateTask{Title: " ", Priority: "unknown"}
	want := fault.Violations{
		{Location: "title", Code: "required", Message: "a task needs a title"},
		{Location: "priority", Code: "not_one_of", Message: `priority "unknown" is not a priority`},
	}
	check := func(t *testing.T, row *contracts.Task, err error) {
		t.Helper()
		if row != nil || !errors.Is(err, fault.ErrInvalid) {
			t.Fatalf("refused create returned row=%+v, error=%v", row, err)
		}
		if got, ok := errors.AsType[fault.Violations](err); !ok || !slices.Equal(got, want) {
			t.Errorf("creator lost validation identity: error=%v, violations=%+v, want=%+v", err, got, want)
		}
	}
	t.Run("fake", func(t *testing.T) {
		row, err := tasktest.NewFake().Create(ctx, db.Tx[db.Tenant]{}, in)
		check(t, row, err)
	})
	t.Run("service", func(t *testing.T) {
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			row, err := internal.NewService().Create(ctx, tx, in)
			check(t, row, err)
			return err
		})
		if !errors.Is(err, fault.ErrInvalid) {
			t.Fatalf("transaction: %v", err)
		}
	})
	for _, table := range []string{"tasks", "platformkit_outbox"} {
		var count int
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("refused creation left %d rows in %s", count, table)
		}
	}
}

func TestTaskAPIKeepsEntityViolationsOnCreateAndPatch(t *testing.T) {
	admin, _, router, _ := mountedTaskTrail(t)
	code, body := call(t, router, http.MethodPost, path, `{"title":"Keep this task"}`)
	if code != http.StatusCreated {
		t.Fatalf("seed: %d %s", code, body)
	}
	rowID := id(t, body)
	want := []fault.Violation{
		{Location: "title", Code: "required", Message: "a task needs a title"},
		{Location: "resolvedAt", Code: "required", Message: "a resolved task needs a resolution time"},
	}
	for _, method := range []string{http.MethodPost, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			at := path
			if method == http.MethodPatch {
				at += "/" + rowID
			}
			code, body := call(t, router, method, at, `{"title":" ","status":"resolved"}`)
			if code != http.StatusUnprocessableEntity {
				t.Fatalf("invalid submission: %d %s", code, body)
			}
			var got problem.Problem
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.Violations, want) {
				t.Errorf("422 discarded entity violations: %s; want %+v", body, want)
			}
		})
	}
	var rows, events int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM tasks").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox").Scan(&events); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || events != 1 {
		t.Errorf("refused writes changed counts: tasks=%d, events=%d; want one of each", rows, events)
	}
	code, body = call(t, router, http.MethodGet, path+"/"+rowID, "")
	var saved contracts.Task
	if err := json.Unmarshal([]byte(body), &saved); err != nil || code != http.StatusOK {
		t.Fatalf("read after refusal: %d %s, %v", code, body, err)
	}
	if saved.Title != "Keep this task" || saved.Status != contracts.StatusOpen || saved.Revision != 1 {
		t.Errorf("refused write changed the task: %+v", saved)
	}
}
