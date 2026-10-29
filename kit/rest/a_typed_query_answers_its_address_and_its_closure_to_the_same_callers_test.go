package rest_test

// A typed query is a read of the resource that declares it, so the address it mounts
// and the closure it registers wear one guard: the mount narrows the Spec's read
// declaration once and hands the same answer to the route and to the httpx.Resource.
// They are arguments of two separate calls, and a mount that gives the address a weaker
// guard than the closure opens a place any signed-in member may query past the
// permission the declaration names, while a page calling that closure in process still
// turns the same member away. No other case asks the pair from one caller's point of
// view: the in-process door is tested on a resource whose guard is that nobody is signed
// in, where the two answers cannot disagree. What is asked here is both answers for the
// member who holds the declared permission and for one who holds nothing, and whether
// the query body ran when the answer was no.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// closureVerdict is the answer of the in-process path: the status the resource's own
// guard gave and what came with it, carried out through a route that stands behind
// nothing but a session, so the route that asks cannot answer for the guard under test.
type closureVerdict struct {
	Status int
	Detail string
	Answer string
}

func TestATypedQueryAnswersItsAddressAndItsClosureToTheSameCallers(t *testing.T) {
	for _, side := range []struct {
		name   string
		grants member
		want   int
		runs   int
	}{
		{"the member who holds the declared permission", member{"query:read": true}, http.StatusOK, 2},
		{"the signed-in member who holds nothing", member{}, http.StatusForbidden, 0},
	} {
		t.Run(side.name, func(t *testing.T) {
			var verdict closureVerdict
			ran := 0
			s := rest.ReadForm(func(_ context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, in queryInputs) (queryResult, error) {
				ran++
				if db.TenantOf(tx).ID != acme.ID || actor != principal {
					t.Error("the query lost the request's tenant or actor")
				}
				return queryResult{in.Title, in.Rank}, nil
			})
			s.Module, s.Entity, s.Path = "query", "query", "/queries"
			s.ReadAuth = httpx.Permission("query:read")
			s.Present = entity.EntryHints{Forms: &entity.FormsHints{
				Input: &entity.FormHints{Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "Show results"}}}}}

			api, router, _ := mountAs(t, s, side.grants, func(api *httpx.API) {
				var query httpx.Resource
				for _, r := range api.Resources() {
					if r.Entity == "query" {
						query = r
					}
				}
				if query.InputRun == nil {
					t.Error("the mount registered no typed query to be called in process")
					return
				}
				httpx.Register(api.Surfaces("query").App, probeOperation("closure", "/closure"), httpx.SignedIn(),
					func(ctx context.Context, _ *struct{}) (*struct{}, error) {
						out, err := query.InputRun(ctx, map[string]any{"title": "Notebook"})
						if err == nil {
							verdict = closureVerdict{Status: http.StatusOK, Answer: fmt.Sprint(out)}
							return nil, nil
						}
						var refusal *problem.Problem
						if !errors.As(err, &refusal) {
							return nil, err
						}
						verdict = closureVerdict{Status: refusal.Status, Detail: refusal.Detail}
						return nil, nil
					})
			})

			code, body := call(t, router, http.MethodPost, "/api/v1/query/queries", `{"title":"Notebook"}`)
			if code != side.want {
				t.Fatalf("the address of the typed query answered %d %s, want %d", code, body, side.want)
			}
			if side.want == http.StatusOK {
				var answer queryResult
				if err := json.Unmarshal([]byte(body), &answer); err != nil || answer.Title != "Notebook" || answer.Rank != 9007199254740993 {
					t.Errorf("the address answered %s, want the answered input and its own default: %v", body, err)
				}
			}
			if side.want != http.StatusOK && !strings.Contains(body, httpx.CodeDenied) {
				t.Errorf("the address answered %d %s, want %s naming the grant that is missing", code, body, httpx.CodeDenied)
			}

			code, body = call(t, router, http.MethodPost, api.Surfaces("query").App.Path("/closure"), "")
			if code != http.StatusNoContent {
				t.Fatalf("the route that calls the closure in process answered %d %s, want it reached", code, body)
			}
			if verdict.Status != side.want {
				t.Errorf("the registered closure answered %d %q where its own address answers %d", verdict.Status, verdict.Detail, side.want)
			}
			if side.want == http.StatusOK && !strings.Contains(verdict.Answer, "Notebook") {
				t.Errorf("the registered closure answered no query result: %+v", verdict)
			}
			if side.want != http.StatusOK && !strings.Contains(verdict.Detail, httpx.CodeDenied) {
				t.Errorf("the registered closure answered %d %q, want %s naming the grant that is missing", verdict.Status, verdict.Detail, httpx.CodeDenied)
			}
			if ran != side.runs {
				t.Errorf("the query body ran %d times, want %d: once through the address and once through the closure, or neither", ran, side.runs)
			}
		})
	}
}
