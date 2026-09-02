package rest_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestMountRegistersTheEntityBesideItsRoutes is the half of stage E4 the
// kernel owns: a Spec that mounts five routes also records what it mounted, so
// that a screen can be derived from it. Before this, Schema() was served to
// nobody.
func TestMountRegistersTheEntityBesideItsRoutes(t *testing.T) {
	api, _, _ := mounted(t)

	resources := api.Resources()
	if len(resources) != 1 {
		t.Fatalf("Mount registered %d resources, want 1", len(resources))
	}
	r := resources[0]
	if r.Module != "tasks" || r.Entity != "task" || r.Path != "/api/tasks" {
		t.Errorf("resource names itself %q/%q at %q", r.Module, r.Entity, r.Path)
	}
	if r.Read != "task:read" || r.Write != "task:write" {
		t.Errorf("resource is guarded by %q/%q, which is not what the Spec declared", r.Read, r.Write)
	}
	// The schema is the entity's, not a copy: a screen renders a select for
	// status because the struct tag says so, and never sees Secret at all.
	if _, ok := crud.FieldNamed(r.Schema.Fields, "secret"); ok {
		t.Error("a json:\"-\" field reached the schema a screen renders")
	}
	f, ok := crud.FieldNamed(r.Schema.Fields, "status")
	if !ok || f.Widget != "select" || len(f.Enum) != 2 {
		t.Errorf("status arrived as %+v, want the select widget and its two values", f)
	}
	if f, ok := crud.FieldNamed(r.Schema.Fields, "id"); !ok || !f.ReadOnly {
		t.Errorf("id arrived as %+v, want read-only", f)
	}
	for _, fn := range []any{r.List, r.Get, r.Create, r.Update, r.Delete} {
		if fn == nil {
			t.Fatal("a registered resource is missing one of its five operations")
		}
	}
}

// TestTheResourceOperationsAreTheRoutesWithoutTheHTTP drives all five closures
// inside one request, which is where a screen calls them: the same transaction,
// the same 404, the same refusal of a field a command owns.
func TestTheResourceOperationsAreTheRoutesWithoutTheHTTP(t *testing.T) {
	immutable := spec
	immutable.Immutable = []string{"priority"}
	api, router, _ := mount(t, immutable)
	r := api.Resources()[0]

	var failures []string
	fail := func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) }

	httpx.Register(api, huma.Operation{
		OperationID: "probe", Method: http.MethodPost, Path: "/probe", Hidden: true,
		DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		created, err := r.Create(ctx, map[string]any{
			"title": "written by a screen", "priority": 3,
			// A screen cannot choose an id any more than a caller can.
			"id": "00000000-0000-0000-0000-000000000001",
		})
		if err != nil {
			fail("Create: %v", err)
			return nil, nil
		}
		id := uuid.MustParse(created["id"].(string))
		if id.String() == "00000000-0000-0000-0000-000000000001" {
			fail("Create honoured the id the form sent")
		}
		if created["title"] != "written by a screen" {
			fail("Create returned %v", created)
		}

		rows, total, err := r.List(ctx, crud.Query{Limit: 10})
		if err != nil || total != 1 || len(rows) != 1 || rows[0]["id"] != created["id"] {
			fail("List = %v, %d, %v", rows, total, err)
		}

		got, err := r.Get(ctx, id)
		if err != nil || got["title"] != created["title"] {
			fail("Get = %v, %v", got, err)
		}
		if _, err := r.Get(ctx, uuid.New()); err == nil || !strings.Contains(err.Error(), "no such row") {
			fail("Get of a row nobody has = %v, want the 404 the route gives", err)
		}

		updated, err := r.Update(ctx, id, map[string]any{"status": "done"})
		if err != nil || updated["status"] != "done" {
			fail("Update = %v, %v", updated, err)
		}
		// The two refusals a form has to be told about, so it can render the
		// field read-only rather than submit it and be surprised.
		if _, err := r.Update(ctx, id, map[string]any{"priority": 9}); err == nil {
			fail("Update wrote a field a command of its own owns")
		}
		if _, err := r.Update(ctx, id, map[string]any{"createdAt": "2020-01-01T00:00:00Z"}); err == nil {
			fail("Update wrote a read-only field")
		}

		if err := r.Delete(ctx, id); err != nil {
			fail("Delete: %v", err)
		}
		if _, err := r.Get(ctx, id); err == nil {
			fail("a deleted row is still readable")
		}
		if err := r.Delete(ctx, id); err == nil {
			fail("deleting a row twice succeeded twice")
		}
		return nil, nil
	})

	if code, body := call(t, router, http.MethodPost, "/probe", ""); code != http.StatusNoContent {
		t.Fatalf("the probe request = %d %s", code, body)
	}
	for _, f := range failures {
		t.Error(f)
	}
}
