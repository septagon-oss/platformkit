package rest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// publishBody is a command with one argument, so the form a screen renders has a
// control in it and the argument arrives through the same decode the JSON route
// used — which is the only reason "an empty note" and "no note" can be one answer.
type publishBody struct {
	Note string `json:"note" validate:"required" doc:"Why this is now visible"`
}

// commands is the mounted world: the row, how many times a command's own closure
// ran, and the item path the screens are being asked about.
type commands struct {
	router chi.Router
	ran    *int
	item   string
}

// mountWithCommands mounts the Spec, then one command that takes an argument and
// one that takes none, then the screens over what the API ended up offering.
//
// The order is the point. Commands are recorded on the API as they are registered,
// so deriving screens before them would render a resource missing doors a module
// added after the fact — and deriving them from the Spec rather than from the API
// is exactly how commands came to have no controls in the first place.
func mountWithCommands(t *testing.T) commands {
	t.Helper()
	api, router, admin := mounted(t)
	ran := new(int)
	rowID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "a draft"); err != nil {
		t.Fatal(err)
	}

	rest.Command(api.Surfaces(spec.Module), spec, "publish", "Publish a note", "Makes it visible to everybody.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in publishBody) (*Task, error) {
			// The refusal is the command's own, with ErrInvalid, which is how
			// rest.Command documents it: "a command whose argument is missing is
			// refused by run, with ErrInvalid, rather than by the decoder". The
			// screen must surface that and not decide for itself what a bad
			// argument means.
			if strings.TrimSpace(in.Note) == "" {
				return nil, fmt.Errorf("%w: note is required to publish", crud.ErrInvalid)
			}
			*ran++
			var out Task
			if err := tx.DB().First(&out, "id = ?", id).Error; err != nil {
				return nil, err
			}
			out.Notes = in.Note
			out.Status = "done"
			return &out, crud.Update(ctx, tx, &out, "notes", "status")
		}, rest.CommandOptions{})

	rest.Command(api.Surfaces(spec.Module), spec, "archive", "Archive a task", "Takes it out of the list.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*Task, error) {
			*ran++
			var out Task
			if err := tx.DB().First(&out, "id = ?", id).Error; err != nil {
				return nil, err
			}
			out.Done = true
			return &out, crud.Update(ctx, tx, &out, "done")
		}, rest.CommandOptions{})

	shell := page.Shell{Tag: "admin", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
		return g.Group(body)
	}}
	for _, r := range api.Resources() {
		if len(r.Commands) != 2 {
			t.Fatalf("the mounted resource carries %d commands, want the two that were registered", len(r.Commands))
		}
		for _, c := range r.Commands {
			// This is the assertion the whole change rests on. Before it, a
			// command was recorded with a verb, a guard and a schema and no work,
			// so a shell could advertise a door it had no way to open — and did,
			// on the native side, for three years.
			if c.Run == nil {
				t.Fatalf("command %q carries no work", c.Verb)
			}
			if !c.Auth.Declared() {
				t.Errorf("command %q arrived with no guard", c.Verb)
			}
		}
		screens.Mount(api.Surfaces(r.Module).App, shell, screens.Options{Workspace: "/app"}, r)
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	return commands{router: router, ran: ran, item: "/app/tasks/task/" + rowID.String()}
}

// TestACommandIsADoorOnTheScreenThatShowsTheRow. Three commands live in
// modules/task today — assign, resolve, check-sla — and none of them has a control
// anywhere a person can press. The route, the permission and the event all exist;
// the generated screen renders fields, an Edit link and a delete form, so resolving
// a task means knowing to POST JSON at a path.
func TestACommandIsADoorOnTheScreenThatShowsTheRow(t *testing.T) {
	c := mountWithCommands(t)

	_, body := call(t, c.router, http.MethodGet, c.item, "")
	for _, want := range []string{
		`action="` + c.item + `/publish"`,
		"Publish a note",
		"Makes it visible to everybody.",
		`name="note"`,
		`action="` + c.item + `/archive"`,
		"Archive a task",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the record page omits %s: %s", want, around(body, "form"))
		}
	}

	code, body, location := postForm(t, c.router, c.item+"/publish", url.Values{"note": {"launch week"}}.Encode())
	if code != http.StatusSeeOther {
		t.Fatalf("the command from a form = %d %s", code, body)
	}
	if location != c.item {
		t.Errorf("after the command the person is at %q, want the record they acted on", location)
	}
	if *c.ran != 1 {
		t.Errorf("the command's own closure ran %d times, want once", *c.ran)
	}
	if _, after := call(t, c.router, http.MethodGet, c.item, ""); !strings.Contains(after, "launch week") {
		t.Errorf("the command's effect is not on the page it redirected to: %s", around(after, "launch"))
	}
}

// TestAnArgumentlessCommandIsAButtonAndNotAGuess: check-sla takes no body, and a
// form that grew an input for it would be the screen guessing what the command
// wants. A button is the honest shape of "do this".
func TestAnArgumentlessCommandIsAButtonAndNotAGuess(t *testing.T) {
	c := mountWithCommands(t)
	_, body := call(t, c.router, http.MethodGet, c.item, "")

	start := strings.Index(body, `/archive"`)
	if start < 0 {
		t.Fatalf("no archive door on the record page")
	}
	end := start + 1200
	if end > len(body) {
		end = len(body)
	}
	archive := body[start:end]
	if strings.Contains(archive, `name="note"`) {
		t.Errorf("the argument-less command was given an argument by the screen: %s", archive)
	}
	code, _, location := postForm(t, c.router, c.item+"/archive", "")
	if code != http.StatusSeeOther || location != c.item {
		t.Errorf("archiving with no body = %d -> %q", code, location)
	}
}

// TestACommandRefusedIsAnsweredTheWayTheAPIRefusesIt: the form must not become a
// second opinion about what a bad argument means. The command's own validation is
// the only rule, reached through the same decode the route uses.
func TestACommandRefusedIsAnsweredTheWayTheAPIRefusedIt(t *testing.T) {
	c := mountWithCommands(t)

	code, body, _ := postForm(t, c.router, c.item+"/publish", url.Values{"note": {""}}.Encode())
	if code == http.StatusSeeOther {
		t.Fatal("an empty note was accepted, so the form bypassed the command's validation")
	}
	if *c.ran != 0 {
		t.Errorf("the closure ran %d times for a refused command", *c.ran)
	}
	if !strings.Contains(strings.ToLower(body), "note") {
		t.Errorf("the refusal does not say which argument it is about: %s", around(body, "note"))
	}
}

// TestTheScreenAndTheAPIRefuseTheSameArgumentTheSameWay is the invariant this whole
// change had to keep: docs/adr/0007 says the screens cannot be more permissive than
// the API, and a command is the first place a form performs a write the API also
// serves. One body, both doors, one answer — because a form that accepted what the
// route refused would put a caller into a state the API says is invalid, and the
// command's own guard would have been decided by which page they were standing on.
//
// The empty note is refused by the command itself with ErrInvalid, which is how
// rest.Command documents argument refusal; the point is not the rule but that both
// doors give the same answer to it.
func TestTheScreenAndTheAPIRefuseTheSameArgumentTheSameWay(t *testing.T) {
	c := mountWithCommands(t)
	_, apiRoute := call(t, c.router, http.MethodPost, "/api/v1/tasks/task/"+strings.TrimPrefix(c.item, "/app/tasks/task/")+"/publish",
		`{"note":""}`)
	_, screenRoute, _ := postForm(t, c.router, c.item+"/publish", url.Values{"note": {""}}.Encode())

	for name, body := range map[string]string{"the API route": apiRoute, "the screen's form": screenRoute} {
		if !strings.Contains(strings.ToLower(body), "note") {
			t.Errorf("%s does not say which argument it refused: %s", name, around(body, "note"))
		}
	}
}
