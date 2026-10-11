package main

// docs/seed.md's worked task example declares `dueAt: "+3d"`, and the brief's
// relative dates are `due: +3d`: a demo tenant seeded today has work due in
// three days. A task record that declares a due date must arrive with it,
// resolved against the run's clock — or the run must refuse the field. A task
// written with no deadline while the plan says CREATE is a seed that dropped
// what its file said.

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/seed"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

type dueDateClock struct{ now time.Time }

func (c dueDateClock) Now() time.Time { return c.now }

func TestSeededTaskCarriesItsRelativeDueDate(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	files := fstest.MapFS{"seed/starter/tasks.yaml": {Data: []byte(
		"apiVersion: platformkit.seed/v1\nresource: tasks\nrecords:\n  - key: welcome-tour\n    fields: {title: Take the tour, priority: normal, dueAt: \"+3d\"}\n")}}
	service, err := seed.New(seed.Deps{Files: files, Root: "seed", Clock: dueDateClock{now: now},
		Writers: []seed.Writer{taskSeeder{svc: c.tasks}}, Authorize: seedGrants{auth: c.auth}})
	if err != nil {
		t.Fatal(err)
	}

	var applied error
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
		tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
		if err != nil {
			return err
		}
		return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			ctx, err = seedActor(ctx, c.users, tx, adminEmail)
			if err != nil {
				return err
			}
			if _, applied = service.Apply(ctx, tx, seed.Selection{}); applied != nil {
				// A refusal of the field is an acceptable answer; it must say so
				// and write nothing, which the rolled-back transaction ensures.
				return applied
			}
			rows, _, err := crud.List[*taskcontracts.Task](tx, crud.Query{Limit: 1, Filter: map[string]any{"title": "Take the tour"}})
			if err != nil {
				return err
			}
			if len(rows) != 1 {
				t.Fatalf("seeded task count = %d; want 1", len(rows))
			}
			want := now.AddDate(0, 0, 3)
			// The instant three days on, or — if the owner resolves a due date as
			// a date — midnight UTC of that day.
			day := time.Date(want.Year(), want.Month(), want.Day(), 0, 0, 0, 0, time.UTC)
			if rows[0].DueAt == nil || (!rows[0].DueAt.Equal(want) && !rows[0].DueAt.Equal(day)) {
				t.Errorf("seeded task dueAt = %v; want %v (the file's +3d against the run's clock)", rows[0].DueAt, want)
			}
			return nil
		})
	}); err != nil && applied == nil {
		t.Fatal(err)
	}
	if applied != nil && !strings.Contains(applied.Error(), "dueAt") {
		t.Errorf("seed run refused with %q; want either the due date applied or a refusal naming dueAt", applied)
	}
}
