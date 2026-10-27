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

// TestACommandSpecNamingItsRouteTwiceRefusesToMount refuses a CommandSpec that
// states the guard or the collection route in Options as well as at the top level.
// Those two facts decide which route exists, so a declaration naming both is a
// command whose route two readers could disagree about.
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
				Options: rest.CommandOptions{Auth: httpx.SignedIn()},
			},
			want: "states its guard twice",
		},
		{
			name: "the collection route",
			cmd: rest.CommandSpec[publishBody]{
				Verb: "publish", Summary: "Publish a note", Doc: "Makes the note visible to the tenant.",
				Collection: true,
				Options:    rest.CommandOptions{Collection: true},
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
