package rest_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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
		// A screen cannot choose an id any more than a caller can, and it
		// cannot reach a field a command of its own owns — priority is
		// Immutable on this Spec, so it is not in this map. See the case below.
		created, err := r.Create(ctx, map[string]any{
			"title": "written by a screen",
			"id":    "00000000-0000-0000-0000-000000000001",
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
		// And the same refusal at the other door: a page that names an
		// immutable field on a create is refused before anything is stored.
		if _, err := r.Create(ctx, map[string]any{"title": "forged", "priority": 9}); err == nil {
			fail("Create wrote a field a command of its own owns")
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

// member is an Authorizer that answers yes to exactly the permissions it holds,
// which is what a role is.
type member map[string]bool

func (m member) Allowed(_ context.Context, _ tenancy.Tenant, want tenancy.Grant) (bool, error) {
	return m[want.Permission], nil
}

// TestTheResourceClosuresCarryTheirOwnAuthorization is the E4 review's critical
// finding: a page that holds a Resource calls its closures directly, so a
// closure that does not ask the Authorizer is a page that reads past the
// permission whenever whoever wrote it forgot to. The dashboard forgot.
//
// The caller here holds task:read and not task:write, which is the ordinary
// shape of a member: the list and the read answer, the three writes refuse with
// the 403 the routes beside them would have given.
func TestTheResourceClosuresCarryTheirOwnAuthorization(t *testing.T) {
	api, router, _ := mountAs(t, spec, member{"task:read": true})
	r := api.Resources()[0]
	if r.Readable(context.Background()) {
		t.Error("a resource reports itself readable to a context with no caller in it")
	}

	var failures []string
	fail := func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) }
	forbidden := func(what string, err error) {
		var p *problem.Problem
		if !errors.As(err, &p) || p.Status != http.StatusForbidden {
			fail("%s = %v, want a 403 problem", what, err)
		}
	}

	httpx.Register(api, huma.Operation{
		OperationID: "probe", Method: http.MethodPost, Path: "/probe", Hidden: true,
		DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if !r.Readable(ctx) || r.Writable(ctx) {
			fail("the resource reports readable=%v writable=%v for a member holding task:read",
				r.Readable(ctx), r.Writable(ctx))
		}
		if _, _, err := r.List(ctx, crud.Query{Limit: 10}); err != nil {
			fail("List refused a caller holding task:read: %v", err)
		}
		if _, err := r.Get(ctx, uuid.New()); errors.Is(err, nil) {
			fail("Get of a row nobody has succeeded")
		}
		// The three writes, each refused before it reaches the database.
		_, err := r.Create(ctx, map[string]any{"title": "written past the permission"})
		forbidden("Create", err)
		_, err = r.Update(ctx, uuid.New(), map[string]any{"title": "renamed past the permission"})
		forbidden("Update", err)
		forbidden("Delete", r.Delete(ctx, uuid.New()))

		// And nothing was written: a refusal that happened after the INSERT
		// would still be a row in the table.
		if _, total, err := r.List(ctx, crud.Query{Limit: 1}); err != nil || total != 0 {
			fail("after three refused writes the table holds %d rows (%v)", total, err)
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

// The helpers a screen turns a Resource into HTML with. They moved here from
// modules/admin, where a second HTML consumer would have had to write them
// again; they are pure, so they are tested without a database.

func TestValuesTypesAFormBySchemaAndRefusesWhatTheFormDidNotOffer(t *testing.T) {
	t.Parallel()
	fields := crud.Fields[*Task]()

	got, err := rest.Values([]byte("title=Chiller&priority=3&status=done&tags=a,+b&dueAt=2026-09-02T14%3A30"), fields, nil)
	if err != nil {
		t.Fatalf("Values: %v", err)
	}
	for name, want := range map[string]any{
		"title": "Chiller", "priority": int64(3), "status": "done", "dueAt": "2026-09-02T14:30:00Z",
		// An unticked checkbox sends nothing, which is the one case where
		// absence is a value.
		"done": false,
	} {
		if fmt.Sprint(got[name]) != fmt.Sprint(want) {
			t.Errorf("%s = %#v, want %#v", name, got[name], want)
		}
	}
	if list, ok := got["tags"].([]string); !ok || strings.Join(list, "|") != "a|b" {
		t.Errorf("tags = %#v", got["tags"])
	}
	// A blank optional field is left out, so a nullable column stays null.
	if _, sent := got["notes"]; sent {
		t.Error("a field the form did not carry was sent as its zero value")
	}
	// A read-only field is never read off a form, whatever it says.
	if _, sent := got["createdAt"]; sent {
		t.Error("a read-only field was read off a form")
	}

	if _, err := rest.Values([]byte("priority=soon"), fields, nil); err == nil {
		t.Error("a word in a number field was accepted")
	}

	// refuse is the create's Immutable: the form does not render those fields,
	// so a value for one did not come from it and is a 422 naming it rather
	// than something quietly dropped.
	_, err = rest.Values([]byte("title=Chiller&priority=9"), fields, []string{"priority"})
	var p *problem.Problem
	if !errors.As(err, &p) || p.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a create carrying an immutable field = %v, want a 422", err)
	}
	if errs, _ := rest.FieldErrors(err, fields); errs["priority"] == "" {
		t.Errorf("the refusal does not mark the control: %v", errs)
	}
	// And a form that simply does not mention it is fine.
	if got, err := rest.Values([]byte("title=Chiller"), fields, []string{"priority"}); err != nil {
		t.Errorf("Values refused a form that left the immutable field out: %v (%v)", err, got)
	}

	// Writable is the update's half: the browser posts a read-only control back
	// and the person changed nothing, so it is dropped rather than refused.
	kept := rest.Writable(map[string]any{"title": "Chiller", "priority": int64(9)}, []string{"priority"})
	if _, still := kept["priority"]; still || kept["title"] != "Chiller" {
		t.Errorf("Writable = %v", kept)
	}
}

// TestFieldErrorsMatchesAFieldNameAndNotASubstringOfOne is the E4 review's
// substring finding: a message about one field marked another's control, which
// is the wrong control with the right message.
func TestFieldErrorsMatchesAFieldNameAndNotASubstringOfOne(t *testing.T) {
	t.Parallel()
	fields := []crud.Field{{Name: "title"}, {Name: "subtitle"}, {Name: "status"}}

	errs, detail := rest.FieldErrors(problem.New(http.StatusUnprocessableEntity,
		"crud: invalid: subtitle is longer than 200 characters"), fields)
	if detail != "subtitle is longer than 200 characters" {
		t.Errorf("detail = %q", detail)
	}
	if _, marked := errs["title"]; marked {
		t.Error("a message about subtitle marked the title control")
	}
	if errs["subtitle"] == "" {
		t.Error("a message about subtitle marked nothing")
	}

	// The explicit shape wins, and is matched exactly.
	p := problem.New(http.StatusUnprocessableEntity, "that could not be saved")
	p.Errors = []string{"status: is not one of open, done", "nosuchfield: ignored"}
	errs, _ = rest.FieldErrors(p, fields)
	if errs["status"] != "is not one of open, done" || len(errs) != 1 {
		t.Errorf("errs = %v", errs)
	}

	// An error that is not a problem is still a message a form can print.
	if _, detail := rest.FieldErrors(errors.New("the database went away"), fields); detail != "the database went away" {
		t.Errorf("a plain error came back as %q", detail)
	}
}

// TestDisplayIsTheOneWayAValueIsShown is the E4 review's consistency finding:
// an enum was humanized in a form's options and raw in a cell, and a boolean
// was the word true in both.
func TestDisplayIsTheOneWayAValueIsShown(t *testing.T) {
	t.Parallel()
	status := crud.Field{Name: "status", Type: crud.TypeString, Enum: []string{"open", "in_progress"}}
	for _, c := range []struct {
		f    crud.Field
		v    any
		want string
	}{
		{status, "in_progress", "In progress"},
		{status, "", "—"},
		{crud.Field{Type: crud.TypeBool}, true, "Yes"},
		{crud.Field{Type: crud.TypeBool}, false, "No"},
		{crud.Field{Type: crud.TypeBool}, nil, "No"},
		{crud.Field{Type: crud.TypeTime}, "2026-09-02T14:30:00.123456Z", "2026-09-02 14:30"},
		{crud.Field{Type: crud.TypeTime}, nil, "—"},
		{crud.Field{Type: crud.TypeString}, "plain", "plain"},
		{crud.Field{Type: crud.TypeInt}, float64(3), "3"},
		{crud.Field{Type: crud.TypeList}, []any{"a", "b"}, "a, b"},
		{crud.Field{Type: crud.TypeString}, nil, "—"},
	} {
		if got := rest.Display(c.f, c.v); got != c.want {
			t.Errorf("Display(%s, %#v) = %q, want %q", c.f.Type, c.v, got, c.want)
		}
	}
	// Text is the other one, and it is not this: a select posts back the enum's
	// own spelling and a checkbox posts "true".
	if rest.Text("in_progress") != "in_progress" || rest.Text(true) != "true" {
		t.Error("Text is rewriting what a control posts")
	}
	for name, want := range map[string]string{
		"slaDeadline": "Sla deadline", "in_progress": "In progress", "status": "Status",
	} {
		if got := rest.Humanize(name); got != want {
			t.Errorf("Humanize(%q) = %q, want %q", name, got, want)
		}
	}
	// A label is the field's name, not its doc: the entities here write a
	// sentence in doc, which reads under a control and not on it.
	f := crud.Field{Name: "slaDeadline", Doc: "Hard SLA deadline; a breach is measured against this"}
	if rest.FieldLabel(f) != "Sla deadline" || rest.FieldHelp(f) != f.Doc {
		t.Errorf("FieldLabel/FieldHelp = %q / %q", rest.FieldLabel(f), rest.FieldHelp(f))
	}
}
