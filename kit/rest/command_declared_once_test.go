package rest_test

// rest.Mount takes one lifecycle command as a declaration; rest.Command takes the
// same command as positional parameters and is the deprecated spelling of it. Both
// build one route, one document entry and one recorded command, because the
// positional form fills in the declaration and hands it to the same body — so "the
// two forms behave identically" is a property of the code, and this case is what
// notices if it stops being one.

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// publishNote is the command both spellings mount: it takes one argument, writes it
// to the row the path names, and publishes the Spec's own update event.
func publishNote(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in publishBody) (*Task, error) {
	var out Task
	if err := tx.DB().First(&out, "id = ?", id).Error; err != nil {
		return nil, err
	}
	out.Notes = in.Note
	return &out, crud.Update(ctx, tx, &out, "notes")
}

// mountPublish mounts the Spec plus publishNote, spelled either way, as one caller,
// and posts the command once: the two mounts are then compared about the same work,
// not about two near-identical descriptions of it. The recorded command comes back
// with its closures cleared, because those are the two fields a second mount is
// entitled to have its own copy of.
func mountPublish(t *testing.T, positional bool, authorize httpx.Authorizer, rowID uuid.UUID, note string) (huma.Operation, httpx.Command) {
	t.Helper()
	api, router, admin := mountAs(t, spec, authorize, func(api *httpx.API) {
		mountPublishRoute(api.Surfaces(spec.Module), positional)
	})
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "a draft"); err != nil {
		t.Fatal(err)
	}
	op := recorded(t, api, "tasks-task-publish")
	if op == nil {
		t.Fatal("the command mounted no route")
	}
	commands := api.Resources()[0].Commands
	if len(commands) != 1 {
		t.Fatalf("the resource records %d commands, want the one", len(commands))
	}
	at := "/api/v1/tasks/task/" + rowID.String() + "/publish"
	if code, body := call(t, router, http.MethodPost, at, `{"note":"`+note+`"}`); code != http.StatusOK || !strings.Contains(body, note) {
		t.Fatalf("POST %s = %d %s, want 200 carrying the note the command wrote", at, code, body)
	}
	mounted := commands[0]
	mounted.Execute, mounted.Run = nil, nil
	return *op, mounted
}

// mountPublishRoute mounts the one command both ways it can be written, from the
// same four facts and no option beyond the Spec's own write guard.
func mountPublishRoute(surfaces httpx.Surfaces, positional bool) {
	if positional {
		rest.Command(surfaces, spec, "publish", "Publish a note", "Makes the note visible to the tenant.",
			[]string{spec.Event(rest.Updated)}, publishNote, rest.CommandOptions{})
		return
	}
	rest.Mount(surfaces, spec, rest.CommandSpec[publishBody]{
		Verb:    "publish",
		Summary: "Publish a note",
		Doc:     "Makes the note visible to the tenant.",
		Emits:   []string{spec.Event(rest.Updated)},
	}, publishNote)
}

// TestASpecMountedBothWaysMountsTheSameCommand mounts one command twice — named
// where it is mounted, as the positional form this package has always taken, and as
// a CommandSpec — and asks whether the document and the recorded command came out
// the same, and whether the route answers the same way for the same caller.
func TestASpecMountedBothWaysMountsTheSameCommand(t *testing.T) {
	const note = "written through both mounts"
	holder := member{"task:read": true, "task:write": true}
	rowID := uuid.New()
	named, namedCommand := mountPublish(t, true, holder, rowID, note)
	declared, declaredCommand := mountPublish(t, false, holder, rowID, note)

	for _, field := range []struct {
		name            string
		named, spokenAt string
	}{
		{"the operation", named.OperationID, declared.OperationID},
		{"the method", named.Method, declared.Method},
		{"the path", named.Path, declared.Path},
		{"the summary", named.Summary, declared.Summary},
		{"the description", named.Description, declared.Description},
	} {
		if field.named != field.spokenAt {
			t.Errorf("the two mounts describe %s differently: %q against %q", field.name, field.named, field.spokenAt)
		}
	}
	if !reflect.DeepEqual(named.Extensions, declared.Extensions) {
		t.Errorf("the two documents publish different guards or events: %v against %v", named.Extensions, declared.Extensions)
	}
	if !reflect.DeepEqual(namedCommand, declaredCommand) {
		t.Errorf("the two mounts recorded different commands:\n%#v\nagainst\n%#v", namedCommand, declaredCommand)
	}
	// And what both say is the command that was written: guarded by the Spec's write
	// permission, at the row route its verb names.
	auth, _ := declared.Extensions[httpx.AuthExtension].(httpx.Auth)
	if got := auth.String(); got != "permission task:write" {
		t.Errorf("the command is guarded by %s, want the Spec's write permission", got)
	}
	const want = "publish Publish a note Makes the note visible to the tenant. /api/v1/tasks/task/{id}/publish"
	if got := declaredCommand.Verb + " " + declaredCommand.Summary + " " + declaredCommand.Description + " " + declaredCommand.Endpoint; got != want {
		t.Errorf("the recorded command is %q, want %q", got, want)
	}

	for _, positional := range []bool{true, false} {
		_, refusing, _ := mountAs(t, spec, member{"task:read": true}, func(api *httpx.API) {
			mountPublishRoute(api.Surfaces(spec.Module), positional)
		})
		at := "/api/v1/tasks/task/" + rowID.String() + "/publish"
		if code, body := call(t, refusing, http.MethodPost, at, `{"note":"written by nobody"}`); code != http.StatusForbidden {
			t.Errorf("a caller without the write grant ran the command at %d %s, want %d", code, body, http.StatusForbidden)
		}
	}
}

// TestACommandSpecNamingItsRouteTwiceRefusesToMount refuses a CommandSpec whose
// Options answers its guard or its collection route differently from the field
// above it. Those two facts decide which route exists, so a declaration answering
// them two ways is a command whose route two readers could disagree about — and it
// is refused before a route exists. A CommandSpec whose two fields answer the same is
// one statement of one fact, whichever field an author wrote it in:
// TestACommandSpecStatingItsGuardInEitherFieldMountsIt and
// TestACommandSpecStatingItsCollectionRouteInBothFieldsMountsIt mount those.
func TestACommandSpecNamingItsRouteTwiceRefusesToMount(t *testing.T) {
	for _, twice := range []struct {
		name string
		cmd  rest.CommandSpec[publishBody]
		want string
	}{
		{
			name: "the guard",
			cmd: rest.CommandSpec[publishBody]{
				Verb: "publish", Summary: "Publish a note", Doc: "Makes the note visible to the tenant.",
				Auth:    httpx.OperatorPermission("task:write"),
				Options: rest.CommandOptions{Auth: httpx.SignedIn()},
			},
			want: "states its guard twice",
		},
		{
			name: "the collection route",
			cmd: rest.CommandSpec[publishBody]{
				Verb: "publish", Summary: "Publish a note", Doc: "Makes the note visible to the tenant.",
				Options: rest.CommandOptions{Collection: true},
			},
			want: "states its route twice",
		},
	} {
		t.Run(twice.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("the command mounted; want a refusal naming %q", twice.want)
					return
				}
				if msg := fmt.Sprint(r); !strings.Contains(msg, twice.want) {
					t.Errorf("the mount site says %q, want it to name %q", msg, twice.want)
				}
			}()
			_, _, _ = mountAs(t, spec, caller{}, func(api *httpx.API) {
				rest.Mount(api.Surfaces(spec.Module), spec, twice.cmd,
					func(context.Context, db.Tx[db.Tenant], uuid.UUID, publishBody) (*Task, error) { return nil, nil })
			})
		})
	}
}

// publishesItsOwnRow is the command the three spellings below mount: it takes the
// row the path names and writes the note it was given.
func publishesItsOwnRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in publishBody) (*Task, error) {
	return publishNote(ctx, tx, id, in)
}

// TestACommandSpecStatingItsGuardInEitherFieldMountsIt mounts one command three
// ways: with its guard at the top level, with the same guard stated in the Options
// the positional form used, and with it stated in both fields. Each states the one
// fact the guard is, so each mounts one command at one address under one guard. The
// middle row is what a migration from rest.Command looks like — an author who moves
// to Mount and keeps the CommandOptions they already wrote has not stated anything
// twice — which is why the rule for a guard is agreement rather than silence about
// the deprecated field. The collection route is not among these three because a bool
// cannot say "nothing" differently from "the row's route": the pair that cannot be
// told apart is refused above, and the pair that agrees is mounted below.
func TestACommandSpecStatingItsGuardInEitherFieldMountsIt(t *testing.T) {
	const doc = "Makes the note visible to the tenant."
	spoken := rest.CommandOptions{Auth: httpx.SignedIn()}
	oneCommand := map[string][2]any{}
	for _, spelled := range []struct {
		name string
		cmd  rest.CommandSpec[publishBody]
	}{
		{"at the top level", rest.CommandSpec[publishBody]{
			Verb: "publish", Summary: "Publish a note", Doc: doc, Auth: httpx.SignedIn(),
		}},
		{"inside the options the positional form used", rest.CommandSpec[publishBody]{
			Verb: "publish", Summary: "Publish a note", Doc: doc, Options: spoken,
		}},
		{"in both fields, answering the same", rest.CommandSpec[publishBody]{
			Verb: "publish", Summary: "Publish a note", Doc: doc,
			Auth: httpx.SignedIn(), Options: spoken,
		}},
	} {
		t.Run(spelled.name, func(t *testing.T) {
			rowID := uuid.New()
			// A member holding nothing runs this command: the guard the author stated is
			// httpx.SignedIn(), and the Spec's write permission is not what admits them.
			api, router, admin := mountAs(t, spec, member{}, func(api *httpx.API) {
				rest.Mount(api.Surfaces(spec.Module), spec, spelled.cmd, publishesItsOwnRow)
			})
			if _, err := admin.ExecContext(t.Context(),
				"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "a draft"); err != nil {
				t.Fatal(err)
			}
			op := recorded(t, api, "tasks-task-publish")
			if op == nil {
				t.Fatal("the command mounted no route")
			}
			at := "/api/v1/tasks/task/" + rowID.String() + "/publish"
			if code, body := call(t, router, http.MethodPost, at, `{"note":"written once"}`); code != http.StatusOK ||
				!strings.Contains(body, "written once") {
				t.Fatalf("POST %s = %d %s, want 200 to a caller holding no permission", at, code, body)
			}
			commands := api.Resources()[0].Commands
			if len(commands) != 1 {
				t.Fatalf("the resource records %d commands, want the one", len(commands))
			}
			mounted := commands[0]
			if got := mounted.Auth.String(); got != "signed_in" {
				t.Errorf("the command is guarded by %s, want the declared signed_in", got)
			}
			if mounted.Collection || mounted.Endpoint != "/api/v1/tasks/task/{id}/publish" {
				t.Errorf("the command is mounted at %s with Collection %t, want the row's route",
					mounted.Endpoint, mounted.Collection)
			}
			mounted.Execute, mounted.Run = nil, nil
			oneCommand[spelled.name] = [2]any{*op, mounted}
		})
	}
	first := oneCommand["at the top level"]
	for name, shape := range oneCommand {
		for _, i := range []int{0, 1} {
			if !reflect.DeepEqual(shape[i], first[i]) {
				t.Errorf("the command spelled %s is not the command mounted at the top level:\n%#v\nagainst\n%#v",
					name, shape[i], first[i])
			}
		}
	}
}

// claimsAnOpenTask is the collection command both spellings below mount. The caller
// names no row, so it takes the oldest open task and writes the note it was given; a
// run handed a row id means the command was mounted at a row's address.
func claimsAnOpenTask(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in publishBody) (*Task, error) {
	if id != uuid.Nil {
		return nil, crud.ErrInvalid
	}
	rows, _, err := crud.List[*Task](tx, crud.Query{Limit: 1, Sort: "title"})
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, crud.ErrNotFound
	}
	task := rows[0]
	task.Done, task.Notes = true, in.Note
	return task, crud.Update(ctx, tx, task, "done", "notes")
}

// TestACommandSpecStatingItsCollectionRouteInBothFieldsMountsIt mounts one collection
// command twice: with CommandSpec.Collection naming the route, and with that same
// fact also written in the deprecated Options.Collection, which is what an author
// moving from rest.Command and copying their whole CommandOptions block ends up with.
// Each states one route, so each mounts one command at the collection's address and
// none at a row's. An author who moves the fact and clears the old one is refused by
// the case above: only the pair that agrees is one statement.
func TestACommandSpecStatingItsCollectionRouteInBothFieldsMountsIt(t *testing.T) {
	const doc = "Takes the oldest open task and writes the note it was given."
	oneCommand := map[string][2]any{}
	for _, spelled := range []struct {
		name string
		cmd  rest.CommandSpec[publishBody]
	}{
		{"named by the declaration alone", rest.CommandSpec[publishBody]{
			Verb: "claim", Summary: "Claim a task", Doc: doc, Collection: true,
		}},
		{"named by both fields, answering the same", rest.CommandSpec[publishBody]{
			Verb: "claim", Summary: "Claim a task", Doc: doc,
			Collection: true, Options: rest.CommandOptions{Collection: true},
		}},
	} {
		t.Run(spelled.name, func(t *testing.T) {
			rowID := uuid.New()
			api, router, admin := mountAs(t, spec, member{"task:read": true, "task:write": true},
				func(api *httpx.API) {
					rest.Mount(api.Surfaces(spec.Module), spec, spelled.cmd, claimsAnOpenTask)
				})
			if _, err := admin.ExecContext(t.Context(),
				"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "oldest"); err != nil {
				t.Fatal(err)
			}
			op := recorded(t, api, "tasks-task-claim")
			if op == nil {
				t.Fatal("the command mounted no route")
			}
			commands := api.Resources()[0].Commands
			if len(commands) != 1 {
				t.Fatalf("the resource records %d commands, want the one", len(commands))
			}
			mounted := commands[0]
			if !mounted.Collection || mounted.Endpoint != "/api/v1/tasks/task/claim" {
				t.Errorf("the command is mounted at %s with Collection %t, want the collection's route",
					mounted.Endpoint, mounted.Collection)
			}
			if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task/claim",
				`{"note":"claimed without a row"}`); code != http.StatusOK || !strings.Contains(body, "claimed without a row") {
				t.Fatalf("POST the collection's route = %d %s, want 200 and the row the command chose", code, body)
			}
			if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task/"+rowID.String()+"/claim", `{"note":"written at a row"}`); code != http.StatusNotFound {
				t.Errorf("POST a row's route = %d %s, want %d: the command was mounted once, at the collection", code, body, http.StatusNotFound)
			}
			mounted.Execute, mounted.Run = nil, nil
			oneCommand[spelled.name] = [2]any{*op, mounted}
		})
	}
	first := oneCommand["named by the declaration alone"]
	for name, shape := range oneCommand {
		for _, i := range []int{0, 1} {
			if !reflect.DeepEqual(shape[i], first[i]) {
				t.Errorf("the command whose route %s names is not the command the declaration names alone:\n%#v\nagainst\n%#v",
					name, shape[i], first[i])
			}
		}
	}
}
