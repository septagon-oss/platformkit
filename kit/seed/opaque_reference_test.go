package seed

import (
	"context"
	"errors"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

type opaqueReferenceWriter struct{ id uuid.UUID }

func (opaqueReferenceWriter) Resource() Resource {
	return Resource{Alias: "parents", Module: "task", Entity: "parent", WriteGrant: "task.parent.write"}
}

func (opaqueReferenceWriter) Target(context.Context, Record, map[string]uuid.UUID, time.Time) (Target, error) {
	return Target{}, errors.New("unexpected target for an absent seed document")
}

func (w opaqueReferenceWriter) Read(_ context.Context, _ db.Tx[db.Tenant], key Key, _ bool) (Snapshot, error) {
	if key.RecordID == w.id {
		return Snapshot{Present: true, ID: w.id}, nil
	}
	return Snapshot{}, nil
}

func (opaqueReferenceWriter) Create(context.Context, db.Tx[db.Tenant], Target) (Snapshot, error) {
	return Snapshot{}, errors.New("unexpected create")
}

func (opaqueReferenceWriter) Update(context.Context, db.Tx[db.Tenant], Snapshot, Target) (Snapshot, error) {
	return Snapshot{}, errors.New("unexpected update")
}

func (opaqueReferenceWriter) Delete(context.Context, db.Tx[db.Tenant], Snapshot) error {
	return errors.New("unexpected delete")
}

func TestReferenceResolvesAnExistingOpaqueSeedKey(t *testing.T) {
	parent := opaqueReferenceWriter{id: uuid.New()}
	tasks := newFakeWriter("tasks", "task", "task", true)
	tasks.resource.References = []Reference{{Resource: "tasks", Path: "fields/parent", Target: "parents"}}
	service, err := New(Deps{
		Files: fstest.MapFS{"seed/starter/tasks.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: tasks\nrecords:\n  - key: current\n    fields: {parent: 'parents/archived'}\n")}},
		Root:  "seed", Clock: seedAt, Writers: []Writer{tasks, parent}, Authorize: &fakeGrant{},
	})
	if err != nil {
		t.Fatal(err)
	}
	seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := putKey(tx, parent.Resource(), "archived", "starter", parent.id); err != nil {
			return err
		}
		plan, err := service.Plan(ctx, tx, Selection{})
		if err != nil {
			t.Errorf("Plan refused a reference to an existing mapped opaque key: %v", err)
		} else if len(plan.Items) != 1 || plan.Items[0].Action != Create {
			t.Errorf("Plan = %+v; want one create after resolving the existing parent", plan.Items)
		}
		return nil
	})
}
