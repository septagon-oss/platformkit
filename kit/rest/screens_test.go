package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

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
	// What a resource records about itself is relative; the address is the
	// surface's. A module that named its own namespace in the path is refused at
	// Mount, and this is the record a generated screen links from.
	if r.Module != "tasks" || r.Entity != "task" || r.Path != "/task" {
		t.Errorf("resource names itself %q/%q at %q", r.Module, r.Entity, r.Path)
	}
	if r.Schema.Path != "/api/v1/tasks/task" || r.Screen != "/app/tasks/task" {
		t.Errorf("the resource's two addresses are %q and %q", r.Schema.Path, r.Screen)
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
	if r.List == nil || r.Get == nil || r.Create == nil || r.Update == nil || r.Delete == nil {
		t.Fatal("a registered resource is missing one of its five operations")
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

	httpx.Register(api.Surfaces("tasks").App, huma.Operation{
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
		if _, err := r.Update(ctx, id, map[string]any{"notes": "Remove this", "dueAt": "2026-09-12T14:30:00Z"}); err != nil {
			fail("setting optional fields: %v", err)
		}
		cleared, err := rest.UpdateValues([]byte("notes=&dueAt="), r.Schema.Fields, nil)
		if err != nil {
			fail("reading cleared controls: %v", err)
		}
		if _, err := r.Update(ctx, id, cleared); err != nil {
			fail("clearing optional fields: %v", err)
		}
		got, err = r.Get(ctx, id)
		if err != nil || rest.Text(got["notes"]) != "" || got["dueAt"] != nil || got["title"] != created["title"] {
			fail("cleared controls did not persist or changed an absent field: %v, %v", got, err)
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

	if code, body := call(t, router, http.MethodPost, api.Surfaces("tasks").App.Path("/probe"), ""); code != http.StatusNoContent {
		t.Fatalf("the probe request = %d %s", code, body)
	}
	for _, f := range failures {
		t.Error(f)
	}
}

// TestTheInProcessDoorsRefuseAFoldedNameByTheSameRule is the kernel's case for
// the doors beneath the HTTP ones. A page reaches the same two writes through
// httpx.Resource closures rather than a request, and the map it hands over is
// built by rest.Values from the schema's own names — except when a module's page
// builds one by hand, which is where a key the person never typed arrives. decode
// and merge both fold as the JSON decoder folds, so the rule the two HTTP doors
// now enforce is the same predicate these two run. It is written here, in the
// kernel, against the helpers every route test in this package already shares
// (mount, call, count), because a rule every module has to re-probe is a rule
// the kernel did not own. The kernel exports no route-test helper for a module
// to inherit — every module writes its own mount and call — so the one case at
// a module's own generated routes lives in that pattern instead: user_test.
// TestALifecycleChangeHasExactlyOneDoor names the folded spellings at
// modules/user's routes, which is what says this predicate runs under a module's
// routes and not only under the kernel's.
func TestTheInProcessDoorsRefuseAFoldedNameByTheSameRule(t *testing.T) {
	owned := spec
	owned.Immutable = []string{"status", "notes"}
	api, router, admin := mount(t, owned)
	r := api.Resources()[0]

	var failures []string
	fail := func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) }
	// The probe is a route of the tasks module on the workspace surface: the
	// address it answers at is composed from that, not written here.
	probe := api.Surfaces(owned.Module).App
	httpx.Register(probe, huma.Operation{
		OperationID: "probe", Method: http.MethodPost, Path: "/probe", Hidden: true,
		DefaultStatus: http.StatusNoContent,
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		created, err := r.Create(ctx, map[string]any{"title": "written by a page"})
		if err != nil {
			fail("Create: %v", err)
			return nil, nil
		}
		at := uuid.MustParse(created["id"].(string))
		for _, sent := range []struct{ field, key string }{
			{"status", "status"}, {"status", "STATUS"}, {"status", "\u017ftatus"},
			{"notes", "notes"}, {"notes", "NOTES"},
		} {
			// A body that mixes the two is refused whole, so the title each
			// create carries differs: one unique violation would abort the
			// request's transaction and answer for every case after it.
			if _, err := r.Create(ctx, map[string]any{"title": "forged " + sent.key, sent.key: "mine"}); err == nil ||
				!strings.Contains(err.Error(), sent.field+" belongs to a route of its own") {
				fail(`Create names %q: %v, want the sentence naming %q`, sent.key, err, sent.field)
			}
			if _, err := r.Update(ctx, at, map[string]any{sent.key: "mine"}); err == nil ||
				!strings.Contains(err.Error(), sent.field+" belongs to a route of its own") {
				fail(`Update names %q: %v, want the sentence naming %q`, sent.key, err, sent.field)
			}
		}
		// The empty write, at the door a page uses: a save that named no column
		// changed no row, so it stamps no row and publishes no event.
		after, err := r.Update(ctx, at, map[string]any{})
		if err != nil {
			fail("Update of nothing: %v", err)
		} else if after["updatedAt"] != created["updatedAt"] {
			fail("an Update that named no column returned a row stamped %v, the create stamped it %v", after["updatedAt"], created["updatedAt"])
		}
		// And none of those refusals stored anything: the door refused the
		// write, it did not strip the field from it and keep the rest.
		if _, total, err := r.List(ctx, crud.Query{Limit: 10}); err != nil || total != 1 {
			fail("the tenant holds %d rows after the refusals, want the one row it created: %v", total, err)
		}
		return nil, nil
	})

	if code, body := call(t, router, http.MethodPost, probe.Path("/probe"), ""); code != http.StatusNoContent {
		fail("the probe request = %d %s", code, body)
	}
	for _, f := range failures {
		t.Error(f)
	}
	if n := count(t, admin, owned.Event(rest.Updated)); n != 0 {
		t.Errorf("the doors beneath HTTP published %d %s events, want none", n, owned.Event(rest.Updated))
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
	api, router, admin := mountAs(t, spec, member{"task:read": true})
	rowID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "readable task"); err != nil {
		t.Fatal(err)
	}
	r := api.Resources()[0]
	if r.Readable(context.Background()) {
		t.Error("a resource reports itself readable to a context with no caller in it")
	}

	var failures []string
	fail := func(format string, args ...any) { failures = append(failures, fmt.Sprintf(format, args...)) }
	forbidden := func(what string, err error) {
		if p, ok := errors.AsType[*problem.Problem](err); !ok || p.Status != http.StatusForbidden {
			fail("%s = %v, want a 403 problem", what, err)
		}
	}

	httpx.Register(api.Surfaces("tasks").App, huma.Operation{
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
		if row, err := r.Get(ctx, rowID); err != nil || row["title"] != "readable task" {
			fail("Get refused or changed a readable row: %v, %v", row, err)
		}
		if _, err := r.Get(ctx, uuid.New()); err != nil {
			if p, ok := errors.AsType[*problem.Problem](err); !ok || p.Status != http.StatusNotFound {
				fail("Get of a missing row = %v, want a 404 problem", err)
			}
		} else {
			fail("Get of a missing row succeeded")
		}
		// The three writes, each refused before it reaches the database.
		_, err := r.Create(ctx, map[string]any{"title": "written past the permission"})
		forbidden("Create", err)
		_, err = r.Update(ctx, rowID, map[string]any{"title": "renamed past the permission"})
		forbidden("Update", err)
		forbidden("Delete", r.Delete(ctx, rowID))

		// And nothing was written: a refusal that happened after the INSERT
		// would still be a row in the table.
		if _, total, err := r.List(ctx, crud.Query{Limit: 1}); err != nil || total != 1 {
			fail("after three refused writes the table holds %d rows (%v)", total, err)
		}
		if row, err := r.Get(ctx, rowID); err != nil || row["title"] != "readable task" {
			fail("refused writes changed the readable row: %v, %v", row, err)
		}
		return nil, nil
	})

	if code, body := call(t, router, http.MethodPost, api.Surfaces("tasks").App.Path("/probe"), ""); code != http.StatusNoContent {
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

func TestUpdateValuesDistinguishesClearedAbsentAndImmutableFields(t *testing.T) {
	t.Parallel()
	fields := []crud.Field{
		{Name: "description", Type: crud.TypeText}, {Name: "dueAt", Type: crud.TypeTime},
		{Name: "tags", Type: crud.TypeList, Elem: crud.TypeString}, {Name: "title", Type: crud.TypeString},
		{Name: "resolution", Type: crud.TypeString}, {Name: "createdAt", Type: crud.TypeTime, ReadOnly: true},
	}
	// New entities and commands retain the existing omission/default contract.
	create, err := rest.Values([]byte("description=&dueAt=&tags="), fields, nil)
	if err != nil || len(create) != 0 {
		t.Fatalf("blank create values changed: %v, %v", create, err)
	}
	got, err := rest.UpdateValues([]byte("description=&dueAt=&tags=&resolution=forged&createdAt="), fields, []string{"resolution"})
	wire, _ := json.Marshal(got)
	if err != nil || string(wire) != `{"description":"","dueAt":null,"tags":[]}` {
		t.Fatalf("edit should clear only the editable fields it carried: %s, %v", wire, err)
	}
}

func TestValuesValidateInstantsWithoutPanicking(t *testing.T) {
	t.Parallel()
	fields := []crud.Field{{Name: "dueAt", Type: crud.TypeTime}}
	for _, input := range []string{"x", "2026-09-12", "2026-02-30T10:00", "2026-09-12T25:00", "2026-09-12T10:00:00Zjunk"} {
		t.Run(input, func(t *testing.T) {
			_, err := rest.Values([]byte("dueAt="+input), fields, nil)
			p, ok := err.(*problem.Problem)
			if !ok || p.Status != http.StatusUnprocessableEntity {
				t.Fatalf("invalid instant = %v, want a 422", err)
			}
			if errs, _ := rest.FieldErrors(err, fields); errs["dueAt"] == "" {
				t.Fatal("the invalid instant did not identify its control")
			}
		})
	}
	for input, want := range map[string]string{
		"2026-09-12T14:30": "2026-09-12T14:30:00Z", "2026-09-12T14:30:15": "2026-09-12T14:30:15Z",
		"2026-09-12T14:30:15.123Z": "2026-09-12T14:30:15.123Z", "2026-09-12T14:30:00-04:00": "2026-09-12T14:30:00-04:00",
		"2026-09-12T14:30:00%2B05:30": "2026-09-12T14:30:00+05:30",
	} {
		got, err := rest.Values([]byte("dueAt="+input), fields, nil)
		if err != nil {
			t.Fatalf("valid instant %q: %v", input, err)
		}
		if _, err := time.Parse(time.RFC3339Nano, got["dueAt"].(string)); err != nil {
			t.Fatalf("instant %q is not on the API wire: %v", got["dueAt"], err)
		}
		if got["dueAt"] != want {
			t.Fatalf("instant %q became %q, want %q", input, got["dueAt"], want)
		}
	}
}

func TestValuesPreserveTypedListElementsAndClearLists(t *testing.T) {
	t.Parallel()
	fields := []crud.Field{
		{Name: "counts", Type: crud.TypeList, Elem: crud.TypeInt},
		{Name: "ratios", Type: crud.TypeList, Elem: crud.TypeFloat},
		{Name: "enabled", Type: crud.TypeList, Elem: crud.TypeBool},
	}
	got, err := rest.Values([]byte("counts=1,+-2&ratios=0.25,+-1.5&enabled=true,+false"), fields, nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Counts  []int     `json:"counts"`
		Ratios  []float64 `json:"ratios"`
		Enabled []bool    `json:"enabled"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatalf("form values cannot reach their typed command argument: %v", err)
	}
	if fmt.Sprint(decoded) != "{[1 -2] [0.25 -1.5] [true false]}" {
		t.Fatalf("typed values changed: %s", wire)
	}
	for _, input := range []string{"counts=1,2.5", "ratios=0.5,soon", "ratios=NaN", "ratios=Inf", "enabled=true,maybe"} {
		if _, err := rest.Values([]byte(input), fields, nil); err == nil {
			t.Errorf("invalid list %q was accepted", input)
		}
	}
	got, err = rest.UpdateValues([]byte("counts="), fields, nil)
	wire, _ = json.Marshal(got)
	if err != nil || string(wire) != `{"counts":[]}` {
		t.Fatalf("clearing a list = %s, %v", wire, err)
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
	p.Status = http.StatusConflict
	errs, _ = rest.FieldErrors(p, fields)
	if errs["status"] != "is not one of open, done" || len(errs) != 1 {
		t.Errorf("a conflict discarded its explicit field errors: %v", errs)
	}

	// An error that is not a problem is still a message a form can print.
	if _, detail := rest.FieldErrors(errors.New("the database went away"), fields); detail != "the database went away" {
		t.Errorf("a plain error came back as %q", detail)
	}
}

const uniqueConflictDetail = "A record already uses one of these values. Change the duplicate value and try again."

func TestFaultKeepsUniqueAndBusinessConflictsDistinct(t *testing.T) {
	unique := &crud.UniqueConflict{Constraint: "internal_title_key"}
	for _, tt := range []struct {
		err    error
		status int
		detail string
		fields int
	}{
		{unique, http.StatusConflict, uniqueConflictDetail, 0},
		{fmt.Errorf("private context: %w", unique), http.StatusConflict, uniqueConflictDetail, 0},
		{fmt.Errorf("%w: the title cannot change until unpublished", crud.ErrConflict), http.StatusConflict, "crud: conflict: the title cannot change until unpublished", 0},
		{crud.ErrConflict, http.StatusConflict, "crud: conflict", 0},
		{fmt.Errorf("%w: title is required", crud.ErrInvalid), http.StatusUnprocessableEntity, "crud: invalid: title is required", 1},
		{fmt.Errorf("read: %w", crud.ErrNotFound), http.StatusNotFound, "no such row, or none this tenant may see", 0},
	} {
		got := rest.Fault(tt.err)
		p, ok := errors.AsType[*problem.Problem](got)
		if !ok || p.Status != tt.status || p.Detail != tt.detail {
			t.Fatalf("Fault(%v) = %v; want %d %q", tt.err, got, tt.status, tt.detail)
		}
		fields, detail := rest.FieldErrors(got, []crud.Field{{Name: "title"}, {Name: "value"}})
		if len(fields) != tt.fields || detail != strings.TrimPrefix(tt.detail, "crud: invalid: ") {
			t.Errorf("form feedback = %v, %q; want %d field errors and the same detail", fields, detail, tt.fields)
		}
	}
	for _, err := range []error{nil, errors.New("offline")} {
		if !errors.Is(rest.Fault(err), err) {
			t.Errorf("Fault changed an unclassified error: %v", err)
		}
	}
}

// TestDisplayIsTheOneWayAValueIsShown is the E4 review's consistency finding:
// an enum was humanized in a form's options and raw in a cell, and a boolean
// was the word true in both.
