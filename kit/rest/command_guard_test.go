package rest_test

// This file exists because of a review, and says so.
//
// The commit that mounted commands onto screens claimed each route carries "the
// command's own guard", and that the screen shows only what its caller may do. Both
// sentences were written by the person who wrote the code, and no test asked a caller
// who does not hold the grant what they actually receive. The claim was a description
// of intent, not a verified behaviour, and the class of bug it hides is the worst one
// available in a permissions system: a door that is invisible and still open.
//
// So: three questions, each answerable only by running the mounted application.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// denies is the shape of an ordinary member: allowed everything the entity's own
// routes need, refused one named permission. Not an authorizer that refuses
// everything — that would pass a test in which no control renders at all, and prove
// nothing about a particular door being hidden.
type denies struct{ permission string }

func (d denies) Allowed(_ context.Context, _ tenancy.Tenant, grant tenancy.Grant) (bool, error) {
	return grant.Permission != d.permission, nil
}

// guardedWorld is the mounted app, how many times a command's own closure ran, and
// the two paths that lead to the same command: the screen's and the API's.
type guardedWorld struct {
	router  chi.Router
	ran     *int
	screen  string
	apiItem string
}

// mountGuarded mounts the API as a caller refused `permission`, registers one command
// guarded by that permission and one guarded by no more than being signed in, and
// mounts the screens over what the API ended up offering.
//
// Two commands rather than one is the point: a refused door disappearing from the page
// means nothing unless a permitted one is visibly still there, because otherwise "no
// controls rendered" and "the right control hidden" are the same observation.
func mountGuarded(t *testing.T, permission string) guardedWorld {
	t.Helper()
	api, router, admin := mountAs(t, spec, denies{permission: permission})
	ran := new(int)
	rowID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "guarded draft"); err != nil {
		t.Fatal(err)
	}

	rest.Command(api, spec, "publish", "Publish a note", "Makes it visible to everybody.", nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in publishBody) (*Task, error) {
			*ran++
			var out Task
			if err := tx.DB().First(&out, "id = ?", id).Error; err != nil {
				return nil, err
			}
			out.Notes = in.Note
			return &out, crud.Update(ctx, tx, &out, "notes")
		}, rest.CommandOptions{Auth: httpx.Permission("task:publish")})

	rest.Command(api, spec, "archive", "Archive a task", "Takes it out of the list.", nil,
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
		screens.Mount(api, shell, screens.Options{Root: "/admin"}, r)
	}
	return guardedWorld{
		router:  router,
		ran:     ran,
		screen:  "/admin/tasks/task/" + rowID.String(),
		apiItem: "/api/tasks/" + rowID.String(),
	}
}

// TestACallerWithoutTheGuardIsNotOfferedTheDoor. The screen is derived per caller: what
// it shows is what that caller may do, not what the resource is able to do.
func TestACallerWithoutTheGuardIsNotOfferedTheDoor(t *testing.T) {
	w := mountGuarded(t, "task:publish")

	_, body := call(t, w.router, http.MethodGet, w.screen, "")
	if strings.Contains(body, `action="`+w.screen+`/publish"`) {
		t.Errorf("a caller refused %q was offered the door anyway: %s", "task:publish", around(body, "publish"))
	}
	// The door that was NOT refused has to be present, or the assertion above is
	// satisfied by a page that renders no commands at all.
	if !strings.Contains(body, `action="`+w.screen+`/archive"`) {
		t.Fatalf("the permitted door is missing too, so nothing is proven either way: %s", around(body, "form"))
	}
}

// TestACallerWithoutTheGuardCannotPostTheDoorAnyway is the one that matters. A hidden
// control is presentation; a refused route is authorisation. Hiding without refusing is
// the bug this file was written to find, and the one an author is most likely to leave
// in, because the page looks right.
func TestACallerWithoutTheGuardCannotPostTheDoorAnyway(t *testing.T) {
	w := mountGuarded(t, "task:publish")

	// The screen's door takes a form body, the API's takes JSON. Both are asked, in
	// their own encoding, because one guard opening two doors has to answer the same
	// way through both — and comparing a form post to a JSON post would be a test
	// that confuses its own encodings rather than the code's answer.
	code, body, location := postForm(t, w.router, w.screen+"/publish", url.Values{"note": {"publish me anyway"}}.Encode())
	if code == http.StatusSeeOther || code == http.StatusOK {
		t.Errorf("the screen performed a command its caller may not perform: %d -> %q", code, location)
	}
	if code != http.StatusForbidden {
		t.Errorf("the screen answered %d, where the kernel's answer to a refused permission is 403: %s", code, around(body, "forbidden"))
	}
	if *w.ran != 0 {
		t.Errorf("the command's own closure ran %d times for a caller it must refuse", *w.ran)
	}

	apiCode, apiBody := call(t, w.router, http.MethodPost, w.apiItem+"/publish", `{"note":"publish me anyway"}`)
	if apiCode != http.StatusForbidden {
		t.Errorf("the API answered %d for the same command the screen refused: %s", apiCode, around(apiBody, "problem"))
	}

	// And the door this caller MAY use still opens, in the same mount, from the same
	// caller — so a refusal cannot be passed by breaking every command.
	if ok, _, _ := postForm(t, w.router, w.screen+"/archive", ""); ok != http.StatusSeeOther {
		t.Errorf("refusing one door also broke the other: archive answered %d", ok)
	}
	if *w.ran != 1 {
		t.Errorf("the permitted command ran %d times, want once", *w.ran)
	}
}

// TestACommandNamedAfterAScreenVerbRefusesToMount. The screens mount list, new, read,
// edit, update and delete; a command carrying one of those verbs adds a second route
// over a path shape that already has one. Album once shipped unable to mount at all
// because two routes shared an operation id, and the audit that found it never called
// Routes. The refusal is in the mount so the class is closed rather than the instance,
// and this test is here so the refusal is not a comment.
func TestACommandNamedAfterAScreenVerbRefusesToMount(t *testing.T) {
	panicked := ""
	defer func() {
		if panicked == "" {
			t.Error("a command named after a screen verb mounted quietly, so two routes answer one path shape")
		}
	}()

	api, _, _ := mounted(t)
	rest.Command(api, spec, "edit", "Edit it", "A verb the screens already own.", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil },
		rest.CommandOptions{})

	defer func() {
		if r := recover(); r != nil {
			panicked = "recovered"
		}
	}()
	shell := page.Shell{Tag: "admin", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node {
		return g.Group(body)
	}}
	for _, r := range api.Resources() {
		screens.Mount(api, shell, screens.Options{Root: "/admin"}, r)
	}
}
