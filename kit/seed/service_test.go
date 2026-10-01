package seed

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// fixedTime is the Clock a case pins: one instant, read once per invocation.
type fixedTime time.Time

func (f fixedTime) Now() time.Time { return time.Time(f) }

var seedAt = fixedTime(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC))

// fakeWriter is the owner's half of the port, held in memory. The service's
// decisions — what it refuses, what it asks for, what it records — are the
// subject; the row behind them is an owner's own business and every owner here
// is a map. Target puts the record's key under the resource's natural-key field
// the way an owner's create path reads it, because Target is the only thing a
// Create is handed.
type fakeWriter struct {
	resource Resource
	rows     map[string]Snapshot
	writes   []string
}

func newFakeWriter(alias, module, entity string, prunable bool, commands ...string) *fakeWriter {
	return &fakeWriter{resource: Resource{
		Alias: alias, Module: module, Entity: entity, NaturalKey: "slug",
		WriteGrant: module + "." + entity + ".write", Prunable: prunable, Commands: commands,
	}, rows: map[string]Snapshot{}}
}

func (f *fakeWriter) Resource() Resource { return f.resource }

func (f *fakeWriter) Target(_ context.Context, record Record, _ map[string]uuid.UUID, _ time.Time) (Target, error) {
	fields := maps.Clone(record.Fields)
	if fields == nil {
		fields = map[string]any{}
	}
	fields[f.resource.NaturalKey] = record.Key
	commands := map[string]any{}
	for _, command := range record.Commands {
		commands[command.Name] = command.Args
	}
	return Target{Fields: fields, Commands: commands}, nil
}

func (f *fakeWriter) Read(_ context.Context, _ db.Tx[db.Tenant], key Key, _ bool) (Snapshot, error) {
	row, ok := f.rows[key.Value]
	if !ok {
		return Snapshot{}, nil
	}
	row.Present = true
	return row, nil
}

func (f *fakeWriter) Create(_ context.Context, _ db.Tx[db.Tenant], target Target) (Snapshot, error) {
	value, _ := target.Fields[f.resource.NaturalKey].(string)
	if _, exists := f.rows[value]; exists {
		return Snapshot{}, errors.New("fake owner: " + value + " already exists")
	}
	f.rows[value] = Snapshot{Present: true, ID: uuid.New(),
		Fields: maps.Clone(target.Fields), Commands: maps.Clone(target.Commands)}
	f.writes = append(f.writes, "create "+value)
	return f.rows[value], nil
}

func (f *fakeWriter) Update(_ context.Context, _ db.Tx[db.Tenant], current Snapshot, target Target) (Snapshot, error) {
	value, _ := target.Fields[f.resource.NaturalKey].(string)
	current.Fields = maps.Clone(target.Fields)
	current.Commands = maps.Clone(target.Commands)
	f.rows[value] = current
	f.writes = append(f.writes, "update "+value)
	return current, nil
}

func (f *fakeWriter) Delete(_ context.Context, _ db.Tx[db.Tenant], current Snapshot) error {
	value, _ := current.Fields[f.resource.NaturalKey].(string)
	delete(f.rows, value)
	f.writes = append(f.writes, "delete "+value)
	return nil
}

// fakeGrant is the Authorizer the composition is required to hand the service:
// the same grant check a request passes, told per resource and action.
type fakeGrant struct {
	refused error
	calls   []string
}

func (g *fakeGrant) Check(_ context.Context, _ db.Tx[db.Tenant], r Resource, action Action) error {
	g.calls = append(g.calls, string(action)+" "+r.Module+"/"+r.Entity)
	return g.refused
}

// seedTenant opens one tenant's transaction, the only shape a run is legal in.
func seedTenant(t *testing.T, demo bool, fn func(context.Context, db.Tx[db.Tenant]) error) {
	t.Helper()
	admin, app := dbtest.Schema(t)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme", Demo: demo}
	if _, err := admin.ExecContext(t.Context(), `INSERT INTO tenants (id, slug, name) VALUES ($1, $2, $2)`,
		tenant.ID, tenant.Slug); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), app, fn); err != nil {
		t.Fatal(err)
	}
}

// TestServiceRefusesBeforeWritingAnything pins the six refusals a run cannot
// be allowed to survive into a write: a resource nobody offered a writer for,
// a file that asks to prune a resource whose owner has no delete path, a
// command the owner never declared, an actor the owner's grant refuses, a
// natural-key row the seed does not own, and demo records asked for by a tenant
// whose own flag says it is not one. Every leg asserts the same three things
// after the error — it names what it refused, the owner was never asked to
// write, and no provenance row was left behind — because a refused mutation
// writes nothing, emits nothing and returns nothing stale.
func TestServiceRefusesBeforeWritingAnything(t *testing.T) {
	for _, tc := range []struct {
		name, file, body, want string
		writers                func() (*fakeWriter, []Writer)
		refused                error
		seed                   func(*fakeWriter)
	}{
		{
			name: "no writer for a resource",
			file: "seed/starter/sites.yaml",
			body: "apiVersion: platformkit.seed/v1\nresource: sites\nrecords:\n  - key: home\n    fields: {title: Home}\n",
			want: "no writer for sites",
			writers: func() (*fakeWriter, []Writer) {
				owner := newFakeWriter("contents", "content", "content", true, "publish")
				return owner, []Writer{owner}
			},
		},
		{
			name: "prune with no owner delete path",
			file: "seed/starter/contents.yaml",
			body: "apiVersion: platformkit.seed/v1\nresource: contents\nprune: true\nrecords:\n  - key: home\n    fields: {title: Home}\n",
			want: "contents has no owner delete path",
			writers: func() (*fakeWriter, []Writer) {
				owner := newFakeWriter("contents", "content", "content", false, "publish")
				return owner, []Writer{owner}
			},
		},
		{
			name: "a command the writer never declared",
			file: "seed/starter/contents.yaml",
			body: "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    commands:\n      - {name: publish}\n",
			want: "contents has no command publish",
			writers: func() (*fakeWriter, []Writer) {
				owner := newFakeWriter("contents", "content", "content", true)
				return owner, []Writer{owner}
			},
		},
		{
			name: "the owner grant refuses the actor",
			file: "seed/starter/contents.yaml",
			body: "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    fields: {title: Home}\n",
			want: "grant content.content.update is not held",
			writers: func() (*fakeWriter, []Writer) {
				owner := newFakeWriter("contents", "content", "content", true, "publish")
				return owner, []Writer{owner}
			},
			refused: errors.New("refused: grant content.content.update is not held"),
		},
		{
			name: "an unowned natural-key row",
			file: "seed/starter/contents.yaml",
			body: "apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: home\n    fields: {title: Declared}\n",
			want: "is an unowned natural-key row",
			writers: func() (*fakeWriter, []Writer) {
				owner := newFakeWriter("contents", "content", "content", true, "publish")
				return owner, []Writer{owner}
			},
			seed: func(owner *fakeWriter) {
				owner.rows["home"] = Snapshot{ID: uuid.New(),
					Fields: map[string]any{"title": "Somebody else's row", "slug": "home"}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writer, writers := tc.writers()
			grant := &fakeGrant{refused: tc.refused}
			seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if tc.seed != nil {
					tc.seed(writer)
				}
				service, err := New(Deps{Files: fstest.MapFS{tc.file: {Data: []byte(tc.body)}},
					Root: "seed", Clock: seedAt, Writers: writers, Authorize: grant})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := service.Apply(ctx, tx, Selection{}); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Apply error = %v; want one containing %q", err, tc.want)
				}
				if len(writer.writes) != 0 {
					t.Errorf("the refused run asked the owner to write: %v", writer.writes)
				}
				keys, err := ownedKeys(tx, writer.Resource(), "starter")
				if err != nil {
					t.Fatal(err)
				}
				if len(keys) != 0 {
					t.Errorf("the refused run left %d provenance rows behind", len(keys))
				}
				return nil
			})
		})
	}

	t.Run("demo records for a tenant that is not demo", func(t *testing.T) {
		writer := newFakeWriter("contents", "content", "content", true, "publish")
		grant := &fakeGrant{}
		seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			service, err := New(Deps{Files: fstest.MapFS{
				"seed/starter/contents.yaml": {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords: []")},
				"seed/demo/contents.yaml":    {Data: []byte("apiVersion: platformkit.seed/v1\nresource: contents\nrecords:\n  - key: tour\n    fields: {title: Tour}")},
			}, Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: grant})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.Apply(ctx, tx, Selection{Demo: true}); err == nil ||
				!strings.Contains(err.Error(), "demo records are refused for a non-demo tenant") {
				t.Fatalf("Apply error = %v; want the demo refusal", err)
			}
			if len(grant.calls) != 0 {
				t.Errorf("a run refused for the tenant's own flag checked grants first: %v", grant.calls)
			}
			return nil
		})
	})
}

// TestApplyWritesThroughTheOwnerAndRecordsProvenance is the positive half: Plan
// reads and answers without writing, Apply creates through the owner's own
// write path and records what it now owns, a second Apply of the same file is
// unchanged, and prune removes an owned row the file no longer declares while
// leaving a row it never owned.
func TestApplyWritesThroughTheOwnerAndRecordsProvenance(t *testing.T) {
	file := "seed/starter/contents.yaml"
	declared := "apiVersion: platformkit.seed/v1\nresource: contents\nprune: true\nrecords:\n  - key: home\n    fields: {title: Home}\n"
	writer, grant := newFakeWriter("contents", "content", "content", true), &fakeGrant{}
	seedTenant(t, false, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		files := fstest.MapFS{file: {Data: []byte(declared)}}
		service, err := New(Deps{Files: files, Root: "seed", Clock: seedAt, Writers: []Writer{writer}, Authorize: grant})
		if err != nil {
			t.Fatal(err)
		}
		plan, err := service.Plan(ctx, tx, Selection{})
		if err != nil {
			t.Fatal(err)
		}
		if len(plan.Items) != 1 || plan.Items[0].Action != Create {
			t.Fatalf("plan = %+v", plan.Items)
		}
		if len(writer.writes) != 0 {
			t.Errorf("a plan wrote through the owner: %v", writer.writes)
		}
		if keys, err := ownedKeys(tx, writer.Resource(), "starter"); err != nil || len(keys) != 0 {
			t.Errorf("a plan recorded %d provenance rows, %v", len(keys), err)
		}
		applied, err := service.Apply(ctx, tx, Selection{})
		if err != nil {
			t.Fatal(err)
		}
		if len(applied.Items) != 1 || applied.Items[0].Action != Create || applied.Items[0].RecordID == uuid.Nil {
			t.Fatalf("apply = %+v", applied.Items)
		}
		if row, ok := writer.rows["home"]; !ok || row.Fields["title"] != "Home" || row.Fields["slug"] != "home" {
			t.Errorf("the owner was never asked to create the declared row: %+v", writer.rows)
		}
		keys, err := ownedKeys(tx, writer.Resource(), "starter")
		if err != nil || len(keys) != 1 || keys[0].Value != "home" || keys[0].RecordID != applied.Items[0].RecordID {
			t.Fatalf("provenance after apply = %+v, %v", keys, err)
		}
		if again, err := service.Apply(ctx, tx, Selection{}); err != nil ||
			len(again.Items) != 1 || again.Items[0].Action != Unchanged || len(again.Items[0].Changed) != 0 {
			t.Fatalf("re-apply = %+v, %v; want one unchanged item", again.Items, err)
		}
		if len(writer.writes) != 1 {
			t.Errorf("an unchanged second apply wrote again: %v", writer.writes)
		}

		// A row the seed owns but no longer declares is pruned; a row it never
		// owned is left alone, because a missing record is not a delete order.
		foreign := uuid.New()
		writer.rows["someone-elses-page"] = Snapshot{Present: true, ID: foreign,
			Fields: map[string]any{"title": "Not declared here", "slug": "someone-elses-page"}}
		if err := putKey(tx, writer.Resource(), "retired", "starter", uuid.New()); err != nil {
			t.Fatal(err)
		}
		writer.rows["retired"] = Snapshot{Present: true, ID: uuid.New(),
			Fields: map[string]any{"title": "Retired", "slug": "retired"}}
		pruned, err := service.Apply(ctx, tx, Selection{})
		if err != nil {
			t.Fatal(err)
		}
		if len(pruned.Items) != 2 || pruned.Items[1].Action != Prune || pruned.Items[1].Key != "retired" {
			t.Fatalf("prune answer = %+v", pruned.Items)
		}
		if _, ok := writer.rows["retired"]; ok {
			t.Error("the owned row the file dropped is still there")
		}
		if _, ok := writer.rows["someone-elses-page"]; !ok {
			t.Error("prune deleted a row the seed never owned")
		}
		if keys, err := ownedKeys(tx, writer.Resource(), "starter"); err != nil || len(keys) != 1 || keys[0].Value != "home" {
			t.Fatalf("provenance after prune = %+v, %v", keys, err)
		}
		if !strings.Contains(pruned.String(), "0 created, 0 updated, 1 unchanged, 1 pruned") {
			t.Errorf("plan text = %s", pruned.String())
		}
		return nil
	})
}
