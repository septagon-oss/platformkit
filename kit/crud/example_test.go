package crud_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func ExampleGet() {
	withExampleTasks("get", func(ctx context.Context, conn *db.Conn) error {
		task := exampleTask("Read the guide")
		err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return crud.Create(ctx, tx, task)
		})
		if err != nil {
			return err
		}
		var row *Task
		err = db.Run(tenancy.WithTenant(ctx, acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			row, err = crud.Get[*Task](tx, task.ID)
			return err
		})
		if err != nil {
			return err
		}
		fmt.Println(row.Title)
		err = db.Run(tenancy.WithTenant(ctx, globex), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			row, err = crud.Get[*Task](tx, task.ID)
			return err
		})
		if !errors.Is(err, crud.ErrNotFound) {
			return fmt.Errorf("expected hidden row, got %v", err)
		}
		fmt.Println("other tenant cannot read:", errors.Is(err, crud.ErrNotFound) && row == nil)
		return nil
	})
	// Output:
	// Read the guide
	// other tenant cannot read: true
}

func ExampleList() {
	withExampleTasks("list", func(ctx context.Context, conn *db.Conn) error {
		for _, input := range []struct {
			tenant tenancy.Tenant
			title  string
		}{{acme, "Alpha"}, {acme, "Beta"}, {globex, "Hidden"}} {
			if err := db.Run(tenancy.WithTenant(ctx, input.tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return crud.Create(ctx, tx, exampleTask(input.title))
			}); err != nil {
				return err
			}
		}
		var page []*Task
		var total int64
		err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			page, total, err = crud.List[*Task](tx, crud.Query{Limit: 1, Sort: "title"})
			return err
		})
		if err == nil {
			fmt.Println(page[0].Title)
			fmt.Println("page:", len(page), "total:", total)
		}
		return err
	})
	// Output:
	// Alpha
	// page: 1 total: 2
}

func exampleTask(title string) *Task {
	at := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	return &Task{Base: crud.Base{CreatedAt: at, UpdatedAt: at}, Title: title}
}

func withExampleTasks(name string, work func(context.Context, *db.Conn) error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	err := dbtest.WithSchema(ctx, name, func(ctx context.Context, owner *sql.DB, conn *db.Conn) error {
		tx, err := owner.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return work(ctx, conn)
	})
	if err != nil {
		panic(err)
	}
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExampleGet", "ExampleList")
}
