package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/seed"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

// taskSeeder seeds the work a demonstration opens on. A task is created through
// task.Spec — the module's own write core, the one its POST route runs — and
// assigned through its Assign command, so a seeded row arrives with the same
// validation and the same task.created and task.assigned events a person's click
// produces. The title is the natural key the record is found by, because a task
// is named by its one line and the module says so.
type taskSeeder struct{ svc taskcontracts.Service }

func (taskSeeder) Resource() seed.Resource {
	return seed.Resource{
		Alias: "tasks", Module: "task", Entity: "task",
		NaturalKey: "title", WriteGrant: taskcontracts.PermissionTaskUpdate,
		// No delete path: resolving or closing a task is a person's account of
		// their own work, and a file that stopped declaring a title has no
		// business writing that account.
		Prunable: false,
		// The assignee is a person of these same files, named as
		// users/<address>, so the orderer writes the people before the work that
		// names them and a typo in an address refuses at the record.
		References: []seed.Reference{{Resource: "tasks", Path: "fields/assignee", Target: "users"}},
	}
}

func (taskSeeder) Target(ctx context.Context, r seed.Record, resolved map[string]uuid.UUID, now time.Time) (seed.Target, error) {
	// The deadline first, because it is the one declared value YAML reads into a
	// type of its own: an unquoted year-month-day `dueAt:` arrives as a date, and the
	// answer is the honest refusal. A fixed day on disk is a deadline no rerun can
	// honour — this format resolves a deadline against the run that writes the row —
	// so it is refused in the words that say what to write instead, and not read as
	// the absence of a deadline.
	if _, fixed := r.Fields["dueAt"].(time.Time); fixed {
		return seed.Target{}, errors.New("dueAt: the seed resolves a deadline against the run that writes the row, so it is +3d, +2y or friday 09:00, and an absolute date names a day no rerun can honour")
	}
	// The deadline first, because it is the one declared value YAML reads into a
	// type of its own: an unquoted year-month-day `dueAt:` arrives as a date, and
	// honest refusal is the grammar's own sentence. This format resolves a deadline
	// against the run that writes the row, so a fixed day on disk is one no rerun
	// can honour, and it is refused in the words that say what to write instead —
	// not read as the absence of a deadline, which is what a silent default would
	// hand the record instead.
	if _, fixed := r.Fields["dueAt"].(time.Time); fixed {
		return seed.Target{}, errors.New("dueAt: the seed resolves a deadline against the run that writes the row, so it is +3d, +2y or friday 09:00; an absolute date names a day no rerun can honour")
	}
	text, err := seedTexts(r, "title", "priority", "assignee", "dueAt")
	if err != nil {
		return seed.Target{}, err
	}
	// A task nobody gave a priority to is an ordinary one. The default answers
	// nobody having answered, not a value this run could not read: `priority: 5`
	// is the number 5, and a run that turned it into `normal` would commit the
	// task, map its key and never report the record again.
	priority, given := text["priority"]
	if !given {
		priority = taskcontracts.PriorityNormal
	}
	assignee := uuid.Nil
	if ref := text["assignee"]; ref != "" {
		id, known := resolved[ref]
		if !known {
			return seed.Target{}, fmt.Errorf("assignee %q names no person this seed knows", ref)
		}
		// Assigning is a person's act in this application, and it is decided twice:
		// by the grant the seed's authorizer asks the auth module about, and by
		// policy/task.rego, which is asked with the person as its actor and refuses a
		// run that carries nobody (modules/task/internal/policy.go). A tenant created
		// with its own seed in the same transaction has no person yet, so what this
		// run may ask for is the work, unassigned, and the operator's later run — the
		// one `make seed` names a person for — is what assigns it. Naming the
		// assignee here anyway would put a CREATE line in the plan for a write this
		// run cannot make, which is the thing a plan exists not to do.
		if _, hasPerson := tenancy.PrincipalFrom(ctx); hasPerson {
			assignee = id
		}
	}
	target := seed.Target{Fields: map[string]any{
		"title": text["title"], "priority": priority, "assignee": assignee,
	}}
	// A declared deadline reaches its owner. `dueAt: "+3d"` is the seed format's
	// relative date and the run's clock is what resolves it — the same instant the
	// row's own timestamps came from — so the demonstration's work is due when the
	// file that opened it says it is. It is a create-only fact for the reason
	// kit/seed/decide.go gives: a deadline is a person's field once the task
	// exists, and a value that moves with the clock is never reconciled. A date the
	// grammar does not read is refused here, at the record's own line, and the run
	// writes nothing.
	if expr := text["dueAt"]; expr != "" {
		due, err := seed.ResolveDate(now, expr, false)
		if err != nil {
			return seed.Target{}, fmt.Errorf("dueAt: %w", err)
		}
		target.CreateOnly = map[string]any{"dueAt": due}
	}
	return target, nil
}

func (taskSeeder) Read(ctx context.Context, tx db.Tx[db.Tenant], key seed.Key, forUpdate bool) (seed.Snapshot, error) {
	id := key.RecordID
	if id == uuid.Nil {
		rows, _, err := crud.List[*taskcontracts.Task](tx, crud.Query{Limit: 1, Filter: map[string]any{"title": key.Value}})
		if err != nil {
			return seed.Snapshot{}, err
		}
		if len(rows) == 0 {
			return seed.Snapshot{}, nil
		}
		id = rows[0].ID
	}
	var (
		row *taskcontracts.Task
		err error
	)
	if forUpdate {
		row, err = crud.GetForUpdate[*taskcontracts.Task](tx, id)
	} else {
		row, err = crud.Get[*taskcontracts.Task](tx, id)
	}
	if errors.Is(err, crud.ErrNotFound) {
		return seed.Snapshot{}, nil
	}
	if err != nil {
		return seed.Snapshot{}, err
	}
	return assigned(row), nil
}

func (w taskSeeder) Create(ctx context.Context, tx db.Tx[db.Tenant], t seed.Target) (seed.Snapshot, error) {
	row, err := task.Spec.CreateRow(ctx, tx, &taskcontracts.Task{
		Title: t.Fields["title"].(string), Priority: t.Fields["priority"].(string),
		DueAt: seedTime(t.CreateOnly["dueAt"]),
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.assign(ctx, tx, assigned(row), t)
}

// Update changes the title and priority and, where the file names a different
// person, assigns. The title is in the patch because the file declares it as a
// field of the record and the record is named by its key, not by its title: a
// run that says the tour task is called "Edited title" means the row it already
// wrote to be called something else. The deadline is not in the patch, and for
// the opposite reason: the file declared it as the record's creation date plus a
// span (see Target), and a person who moved it through the task screen moved it
// for good.
func (w taskSeeder) Update(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	row, err := task.Spec.UpdateRow(ctx, tx, cur.ID, map[string]any{
		"title": t.Fields["title"].(string), "priority": t.Fields["priority"].(string),
	})
	if err != nil {
		return seed.Snapshot{}, err
	}
	return w.assign(ctx, tx, assigned(row), t)
}

func (w taskSeeder) assign(ctx context.Context, tx db.Tx[db.Tenant], cur seed.Snapshot, t seed.Target) (seed.Snapshot, error) {
	want, _ := t.Fields["assignee"].(uuid.UUID)
	have, _ := cur.Fields["assignee"].(uuid.UUID)
	if want == uuid.Nil {
		// The file names nobody. On a create that is an unassigned row, which is what
		// a run with no person to assign as may write at all (see Target). On an
		// update it is a record that took the name away — and the module has no
		// command that takes one back: Assign gives work to a person, and putting it
		// back on the pile is whoever holds it saying they no longer have it. So the
		// run refuses in words and writes nothing: patching the row's other fields,
		// publishing an update and leaving the assignee in place would be a seed that
		// reported a change it did not make, on every run, forever.
		if have != uuid.Nil {
			return seed.Snapshot{}, errors.New("task: this file names no assignee, and the module has no command to take one back: the row is assigned, so this file cannot unassign it")
		}
		return cur, nil
	}
	if want == have {
		return cur, nil
	}
	row, err := w.svc.Assign(ctx, tx, cur.ID, want)
	if err != nil {
		return seed.Snapshot{}, err
	}
	return assigned(row), nil
}

func (taskSeeder) Delete(context.Context, db.Tx[db.Tenant], seed.Snapshot) error {
	return errors.New("task: resolving a task is not undoing one")
}

// assigned is a task's canonical seed state: the line a list shows, the urgency
// beside it, and who is responsible — nobody being uuid.Nil, so an unassigned row
// and a record that names no person agree.
func assigned(row *taskcontracts.Task) seed.Snapshot {
	assignee := uuid.Nil
	if row.AssigneeID != nil {
		assignee = *row.AssigneeID
	}
	return seed.Snapshot{Present: true, ID: row.ID,
		Fields: map[string]any{"title": row.Title, "priority": row.Priority, "assignee": assignee}}
}
