package rest_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

type queryInputs struct {
	Title string `json:"title" validate:"required"`
	Rank  int64  `json:"rank" default:"9007199254740993"`
}

type queryResult struct {
	Title string `json:"title"`
	Rank  int64  `json:"rank"`
}

func TestReadFormUsesTypedInputWithoutStorageIdentity(t *testing.T) {
	api, router, _ := mounted(t)
	calls := 0
	s := rest.ReadForm(func(_ context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, in queryInputs) (queryResult, error) {
		calls++
		if db.TenantOf(tx).ID != acme.ID || actor != principal {
			t.Fatal("query lost current tenant or actor")
		}
		return queryResult{in.Title, in.Rank}, nil
	})
	s.Module, s.Entity, s.Path, s.ReadAuth = "query", "query", "/queries", httpx.SignedIn()
	s.Present = entity.EntryHints{Forms: &entity.FormsHints{Input: &entity.FormHints{Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "Show results"}}}}}
	if len(s.Events()) != 0 || len(s.Declared()) != 0 || len(s.Schema().Fields) != 2 {
		t.Fatal("input-only Spec claimed storage")
	}
	s.Mount(api.Surfaces("query"))
	resources := api.Resources()
	r := resources[len(resources)-1]
	if r.InputRun == nil || r.Offers(httpx.CRUDCreate) || r.Offers(httpx.CRUDList) || r.Screen != "/app/query/queries" {
		t.Fatalf("query registration: %+v", r)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/query/queries", `{"title":"Notebook"}`)
	var result queryResult
	if err := json.Unmarshal([]byte(body), &result); err != nil || code != 200 || calls != 1 || result.Rank != 9007199254740993 {
		t.Fatalf("typed output: %d %s %v", code, body, err)
	}
	code, body = call(t, router, http.MethodPost, "/api/v1/query/queries", `{"title":""}`)
	if code != 422 || calls != 1 {
		t.Fatalf("invalid input invoked query: %d %s", code, body)
	}
	for _, method := range []string{"GET", "PATCH", "DELETE"} {
		code, body = call(t, router, method, "/api/v1/query/queries", `{}`)
		if code != 404 && code != 405 {
			t.Fatalf("mounted fake CRUD %s: %d %s", method, code, body)
		}
	}
}

func TestReadFormRefusesStorageDeclarationsAndEntityWriteHelpers(t *testing.T) {
	s := rest.ReadForm(func(context.Context, db.Tx[db.Tenant], uuid.UUID, queryInputs) (queryResult, error) {
		return queryResult{}, nil
	})
	s.Module, s.Entity, s.Path, s.ReadAuth = "query", "query", "/queries", httpx.SignedIn()
	var tx db.Tx[db.Tenant]
	_, created := s.CreateRow(t.Context(), tx, nil)
	_, updated := s.UpdateRow(t.Context(), tx, uuid.Nil, nil)
	_, deleted := s.DeleteRow(t.Context(), tx, uuid.Nil)
	for _, err := range []error{created, updated, deleted} {
		if err == nil || !strings.Contains(err.Error(), "Input-only resources have no writes") {
			t.Fatalf("entity helper did not refuse: %v", err)
		}
	}
	api, _, _ := mounted(t)
	s.WriteAuth = httpx.SignedIn()
	defer func() {
		if recover() == nil {
			t.Fatal("input-only Spec accepted a write guard")
		}
	}()
	s.Mount(api.Surfaces("query"))
}

func TestUpdateStepsRequireAStoredRevision(t *testing.T) {
	api, _, _ := mounted(t)
	s := spec
	s.Present.Forms = &entity.FormsHints{Update: &entity.FormHints{Layout: "steps", Steps: []entity.FormStepHints{{PrimaryLabel: "Save"}}}}
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "authoritative revision") {
			t.Fatalf("missing revision refusal: %v", r)
		}
	}()
	s.Mount(api.Surfaces(s.Module))
}
