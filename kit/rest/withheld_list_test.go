package rest_test

// withheld_list_test.go pins what a withheld `list` costs the rest of the
// projection, in the two places the projection cannot see it from the route
// table alone.
//
// A verb that is not offered is a route that is not registered, and the router
// answers that by itself. What the router cannot answer is the two claims made
// somewhere else: the two closures a dashboard and a generated page call, and
// the pages whose whole address space is derived from the list page this Spec
// never mounted. `kit/rest` states both — Count and List refuse rather than
// count rows nobody can list, and `ui/screens` mounts no page at all for a
// resource with no workspace address — and each is one deleted line away from a
// resource that quietly keeps answering both.

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestACountOfAResourceWithNoListRouteRefusesWhereItsCardWouldAnswer is the
// dashboard's claim: modules/admin draws one clickable card per resource whose
// Count answers, and the card's href is httpx.Resource.Screen — the address of
// the list page a list-less Spec did not mount. A count that answers is
// therefore a card that leads nowhere, so the closure has to refuse.
//
// Both arms are here: the withheld verb refuses, and the same Count on the same
// entity with its list route mounted answers the number. Without the second
// arm the case would pass for a Count that refuses everything.
func TestACountOfAResourceWithNoListRouteRefusesWhereItsCardWouldAnswer(t *testing.T) {
	noList := spec
	noList.Operations = []httpx.CRUD{rest.Read, rest.Create, rest.Update, rest.Delete}
	asked := askedFromProbe(t, noList, "count-probe")
	if asked.probeStatus != http.StatusNoContent {
		t.Fatalf("the probe = %d %s", asked.probeStatus, head(asked.probeBody))
	}
	if asked.readback != "a row with no way to list it" {
		t.Fatalf("the offered read answered %q, so the probe never reached the row", asked.readback)
	}
	if !refused(asked.countErr, http.StatusConflict) {
		t.Errorf("Count = %v, want a 409 naming the withheld list: the dashboard would draw a card to %q, which nothing serves", asked.countErr, asked.screen)
	}
	if !refused(asked.listErr, http.StatusConflict) {
		t.Errorf("List = %v, want a 409 naming the withheld list", asked.listErr)
	}

	// The other arm: with the list route mounted, the same two closures answer.
	fullAPI, fullRouter, fullAdmin := mounted(t)
	seed(t, fullAdmin, "a listed row")
	var total int64
	var fullErr error
	fullRes := fullAPI.Resources()[0]
	httpx.Register(fullAPI.Surfaces("tasks").App, probeOperation("count-probe-full", "/count-probe-full"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			total, fullErr = fullRes.Count(ctx)
			return nil, nil
		})
	if code, body := call(t, fullRouter, http.MethodPost, fullAPI.Surfaces("tasks").App.Path("/count-probe-full"), ""); code != http.StatusNoContent {
		t.Fatalf("the listed probe = %d %s", code, body)
	}
	if fullErr != nil || total != 1 {
		t.Errorf("Count of a resource that offers list = %d, %v, want 1: a Count that always refuses would pass the arm below", total, fullErr)
	}
}

// TestAResourceWithNoListRouteMountsNoPageAtAnyAddress is the second claim: a
// resource with no collection page has no workspace address, and every link a
// generated page writes is built from that address. The record and the two forms
// are for verbs this Spec does offer — the point is that the generator stops at
// the missing address and not at the missing verb, so nothing is served at an
// address a person could arrive at with no navigation to explain it.
func TestAResourceWithNoListRouteMountsNoPageAtAnyAddress(t *testing.T) {
	noList := spec
	noList.Operations = []httpx.CRUD{rest.Read, rest.Create}
	router, _, admin := mountScreened(t, noList)
	row := seed(t, admin, "a row no page shows")

	for _, asked := range []struct{ method, path string }{
		{http.MethodGet, "/app/tasks/task"},
		{http.MethodGet, "/app/tasks/task/" + row},
		{http.MethodGet, "/app/tasks/task/new"},
		{http.MethodPost, "/app/tasks/task"},
	} {
		code, body := call(t, router, asked.method, asked.path, `{"title":"written where no page answers"}`)
		if code == http.StatusOK {
			t.Errorf("%s %s = 200 for a resource that publishes no screen address; the page says %q",
				asked.method, asked.path, head(body))
		}
	}
	// And the API says the same thing from the other side: the reads and the
	// create this Spec did mount still answer, so the pages are absent because
	// there is no address to mount them at, not because the resource is dead.
	if code, _ := call(t, router, http.MethodGet, "/api/v1/tasks/task/"+row, ""); code != http.StatusOK {
		t.Errorf("the offered read of the same resource = %d, want 200", code)
	}
	if code, body := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"through the API"}`); code != http.StatusCreated {
		t.Errorf("the offered create of the same resource = %d %s, want 201", code, body)
	}
}

// TestAWriteToAResourceThatOffersNoCreateWritesNothing is the brief's invariant
// at the door a person would knock on: the withheld verb answers before any
// handler exists, so the write that finds no route also writes nothing. The row
// count is read back over the owner connection, which needs no route of this
// Spec's own.
func TestAWriteToAResourceThatOffersNoCreateWritesNothing(t *testing.T) {
	only := spec
	only.Operations = []httpx.CRUD{rest.List, rest.Read}
	_, router, admin := mount(t, only)

	if code, _ := call(t, router, http.MethodPost, "/api/v1/tasks/task", `{"title":"written where nothing listens"}`); code != http.StatusMethodNotAllowed {
		t.Fatalf("POST the collection of a create-less resource = %d, want %d", code, http.StatusMethodNotAllowed)
	}
	var rows int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM rest_tasks").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Errorf("the refused create left %d row(s) in rest_tasks, want none: a refusal that writes is a write", rows)
	}
}

// answers is what the closures of one mounted Spec answered, called from inside
// a request.
type answers struct {
	screen      string
	countErr    error
	listErr     error
	listTotal   int64
	readback    string
	probeStatus int
	probeBody   string
}

// askedFromProbe mounts a Spec and registers one probe route — the same device
// TestTheClosuresOfAnUnofferedVerbRefuseAndNeverPanic uses — that calls the
// resource's own Count, List and Get in the request the router resolved.
func askedFromProbe(t *testing.T, s rest.Spec[*Task], at string) answers {
	t.Helper()
	api, router, admin := mount(t, s)
	row := seed(t, admin, "a row with no way to list it")
	res := api.Resources()[0]
	out := answers{screen: res.Screen}
	httpx.Register(api.Surfaces("tasks").App, probeOperation(at, "/"+at),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			out.countErr = func() error {
				_, err := res.Count(ctx)
				return err
			}()
			_, out.listTotal, out.listErr = res.List(ctx, crud.Query{Limit: 10})
			// The verb it does offer still reaches the row: the refusal below is
			// about the withheld list, not about a resource nothing reads.
			if r, err := res.Get(ctx, uuid.MustParse(row)); err == nil {
				out.readback, _ = r["title"].(string)
			}
			return nil, nil
		})
	out.probeStatus, out.probeBody = call(t, router, http.MethodPost, api.Surfaces("tasks").App.Path("/"+at), "")
	return out
}

// head is the start of a served body, enough to tell a page from a problem
// document when a case reports that one was served where none should be.
func head(body string) string {
	const keep = 120
	if len(body) > keep {
		return body[:keep] + "…"
	}
	return body
}
