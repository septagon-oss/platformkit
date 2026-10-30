package rest_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

func TestGuidedCompletionAndPartialFinishShareProviderCases(t *testing.T) {
	for _, factory := range []string{"fake", "sql"} {
		t.Run(factory, func(t *testing.T) {
			ctx := tenancy.WithTenant(t.Context(), acme)
			var run func(any) (any, error)
			var read func() entity.GuidedSession
			var count func() int
			if factory == "sql" {
				h := newGuided(t)
				run = func(in any) (any, error) { return h.run(t, acme, in) }
				read = func() entity.GuidedSession { return h.read(t) }
				count = func() int { return h.events(t) }
			} else {
				f := newGuidedFake(ctx)
				run = func(in any) (any, error) { return f.run(ctx, in) }
				read = func() entity.GuidedSession {
					s, err := f.store.Get(ctx, guidedID)
					if err != nil {
						t.Fatal(err)
					}
					return s.Session
				}
				count = f.Events.Count
			}
			first, err := run(guidedInput(0, 1))
			if err != nil {
				t.Fatal(err)
			}
			again, err := run(guidedInput(0, 1))
			if err != nil || !reflect.DeepEqual(first, again) || count() != 1 {
				t.Fatalf("retry: %v %v events=%d", again, err, count())
			}
			for _, in := range []entity.CompleteGuidedSet{guidedInput(0, 2), guidedInput(1, 1), guidedInput(2, 2), {StepID: uuid.New(), SetID: uuid.New(), IfRevision: 2}} {
				before, _ := json.Marshal(read())
				events := count()
				out, err := run(in)
				after, _ := json.Marshal(read())
				if err == nil || out != nil || string(before) != string(after) || count() != events {
					t.Fatalf("refusal leaked a result/effect: %#v %v", out, err)
				}
			}
			if _, err := run(guidedInput(1, 2)); err != nil {
				t.Fatal(err)
			}
			state := read()
			step, set, ok := state.Pair(len(state.Progress.Completed))
			if !ok || step != 1 || set != 1 {
				t.Fatalf("resume = %d/%d %v", step, set, ok)
			}
			receipt, err := run(guidedInput(0, 1))
			if err != nil || !reflect.DeepEqual(first, receipt) || count() != 2 {
				t.Fatal("old receipt became current progress", err)
			}
			out, err := run(entity.FinishGuidedSession{IfRevision: 3})
			if err != nil {
				t.Fatal(err)
			}
			summary := out.(entity.GuidedSummary)
			if summary.CompletedSets != 2 || summary.TotalSets != 4 || summary.Revision != 4 {
				t.Fatalf("partial: %+v", summary)
			}
			again, err = run(entity.FinishGuidedSession{IfRevision: 3})
			if err != nil || !reflect.DeepEqual(out, again) || count() != 3 {
				t.Fatal("finish replay changed state", err)
			}
			if out, err = run(guidedInput(2, 4)); err == nil || out != nil || count() != 3 {
				t.Fatal("finished accepted a new set")
			}
			if err := read().Validate(true); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGuidedSQLTenantIsolationConcurrencyAndRollback(t *testing.T) {
	h := newGuided(t)
	foreign := tenancy.Tenant{ID: uuid.New(), Slug: "foreign"}
	if out, err := h.run(t, foreign, guidedInput(0, 1)); !errors.Is(err, crud.ErrNotFound) || out != nil {
		t.Fatalf("foreign write: %v %v", out, err)
	}
	err := db.Run(tenancy.WithTenant(t.Context(), foreign), h.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		out, err := h.service.load(ctx, tx, principal, guidedID)
		if !reflect.DeepEqual(out, entity.GuidedSession{}) || !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("foreign read: %+v %v", out, err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make([]any, 2)
	failures := make([]error, 2)
	var workers sync.WaitGroup
	for i := range 2 {
		workers.Go(func() { <-start; results[i], failures[i] = h.run(t, acme, guidedInput(0, 1)) })
	}
	close(start)
	workers.Wait()
	if failures[0] != nil || failures[1] != nil || !reflect.DeepEqual(results[0], results[1]) || h.events(t) != 1 {
		t.Fatalf("concurrency: %v %v", results, failures)
	}
	before := h.read(t)
	if _, err := h.admin.ExecContext(t.Context(), `CREATE FUNCTION refuse_guided_event() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.name LIKE 'practice.%' THEN RAISE EXCEPTION 'outbox unavailable'; END IF; RETURN NEW; END $$; CREATE TRIGGER refuse_guided_event BEFORE INSERT ON platformkit_outbox FOR EACH ROW EXECUTE FUNCTION refuse_guided_event()`); err != nil {
		t.Fatal(err)
	}
	for _, in := range []any{guidedInput(1, 2), entity.FinishGuidedSession{IfRevision: 2}} {
		if out, err := h.run(t, acme, in); err == nil || out != nil {
			t.Fatal("outbox refusal returned success", out, err)
		}
		if !reflect.DeepEqual(before, h.read(t)) || h.events(t) != 1 {
			t.Fatal("outbox failure committed partial state")
		}
	}
}

func mountGuidedScreens(harness guidedHarness) {
	shell := page.Shell{Tag: "practice", Frame: func(_ context.Context, _ page.Request, nodes []g.Node) g.Node { return h.Main(g.Group(nodes)) }}
	for _, r := range harness.api.Resources() {
		screens.Mount(harness.api.Surfaces("practice").App, shell, screens.Options{Workspace: "/app"}, r)
	}
}

func TestGuidedNativeFormsResumeAndCachedAuthority(t *testing.T) {
	fixture := newGuided(t)
	mountGuidedScreens(fixture)
	api := "/api/v1/practice/sessions/" + guidedID.String()
	web := "/app/practice/sessions/" + guidedID.String()
	get := guidedCall(fixture.router, "GET", web, "", "", "")
	if get.Code != 303 || !strings.Contains(get.Header().Get("Location"), "/steps/") {
		t.Fatalf("resume: %d %s", get.Code, get.Body.String())
	}
	get = guidedCall(fixture.router, "GET", get.Header().Get("Location"), "", "", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), `name="ifRevision" value="1"`) {
		t.Fatalf("step: %d %s", get.Code, get.Body.String())
	}
	for i := range 2 {
		in := guidedInput(i, int64(i+1))
		body := url.Values{"stepId": {in.StepID.String()}, "setId": {in.SetID.String()}, "ifRevision": {fmt.Sprint(i + 1)}}.Encode()
		res := guidedCall(fixture.router, "POST", web+"/complete-set", body, "application/x-www-form-urlencoded", "")
		if res.Code != 303 {
			t.Fatalf("native completion: %d %s", res.Code, res.Body.String())
		}
	}
	get = guidedCall(fixture.router, "GET", web, "", "", "")
	if !strings.HasSuffix(get.Header().Get("Location"), guidedInput(2, 3).SetID.String()) {
		t.Fatal("resume ignored saved set", get.Header())
	}
	res := guidedCall(fixture.router, "POST", web+"/finish", "ifRevision=3", "application/x-www-form-urlencoded", "finish-fixture")
	if res.Code != 303 || fixture.events(t) != 3 {
		t.Fatalf("finish: %d %s", res.Code, res.Body.String())
	}
	get = guidedCall(fixture.router, "GET", web+"/summary", "", "", "")
	if get.Code != 200 || !strings.Contains(get.Body.String(), "Completed 2 of 4 sets") {
		t.Fatalf("summary: %d %s", get.Code, get.Body.String())
	}
	if _, err := fixture.admin.ExecContext(t.Context(), "UPDATE guided_runs SET active=false"); err != nil {
		t.Fatal(err)
	}
	replay := guidedCall(fixture.router, "POST", web+"/finish", "ifRevision=3", "application/x-www-form-urlencoded", "finish-fixture")
	if replay.Code != 403 || strings.Contains(replay.Body.String(), "Session ended") {
		t.Fatalf("cached authority: %d %s", replay.Code, replay.Body.String())
	}
	get = guidedCall(fixture.router, "GET", api, "", "", "")
	if get.Code != 404 {
		t.Fatalf("revoked read: %d %s", get.Code, get.Body.String())
	}
}

func TestGuidedClosedInputsAndOutputValidation(t *testing.T) {
	fixture := newGuided(t)
	path := "/api/v1/practice/sessions/" + guidedID.String() + "/complete-set"
	valid, _ := json.Marshal(guidedInput(0, 1))
	body := string(valid)
	for _, bad := range []string{strings.TrimSuffix(body, "}") + `,"actor":"forged"}`, strings.TrimSuffix(body, "}") + `,"ifRevision":1}`, `{}`, strings.Replace(body, `"ifRevision":1`, `"ifRevision":0`, 1)} {
		response := guidedCall(fixture.router, "POST", path, bad, "application/json", "")
		if response.Code != 422 && response.Code != 400 {
			t.Fatalf("bad input accepted: %d %s", response.Code, response.Body.String())
		}
		if fixture.events(t) != 0 || fixture.read(t).Progress.Revision != 1 {
			t.Fatal("bad input wrote")
		}
	}
	response := guidedCall(fixture.router, "POST", path, body, "application/json", "json-fixture")
	if response.Code != 200 || fixture.events(t) != 1 {
		t.Fatalf("json: %d %s", response.Code, response.Body.String())
	}
	replay := guidedCall(fixture.router, "POST", path, body, "application/json", "json-fixture")
	if replay.Code != 200 || replay.Body.String() != response.Body.String() || fixture.events(t) != 1 {
		t.Fatalf("json replay: %d %s", replay.Code, replay.Body.String())
	}
}
