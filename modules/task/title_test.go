package task_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// Exercise the real module's JSON routes and the resource a generated form
// uses. Direct CRUD must refuse the same input before Postgres rejects it.
func TestTitleLimitsAgreeAcrossWritePaths(t *testing.T) {
	api, router := mounted(t)
	code, body := call(t, router, http.MethodPost, path, `{"title":"seed"}`)
	if code != http.StatusCreated {
		t.Fatalf("seed = %d %s", code, body)
	}
	rowID := uuid.MustParse(id(t, body))
	r := api.Resources()[0]
	type input struct {
		Door string `path:"door"`
		Body struct {
			Title string `json:"title"`
		}
	}
	type output struct{ Body any }
	httpx.Register(surfacesOf(api).App, huma.Operation{OperationID: "title-probe", Method: http.MethodPost, Path: "/probe/{door}", Hidden: true},
		httpx.SignedIn(), func(ctx context.Context, in *input) (*output, error) {
			values := map[string]any{"title": in.Body.Title}
			var result any
			var err error
			switch in.Door {
			case "resource-create":
				result, err = r.Create(ctx, values)
			case "resource-update":
				result, err = r.Update(ctx, rowID, values)
			case "crud-create":
				tx, _ := httpx.TxFrom(ctx)
				e := &contracts.Task{Title: in.Body.Title}
				err = crud.Create(ctx, tx, e)
				result = e
			case "crud-update":
				tx, _ := httpx.TxFrom(ctx)
				e, loadErr := crud.Get[*contracts.Task](tx, rowID)
				if loadErr != nil {
					return nil, rest.Fault(loadErr)
				}
				e.Title = in.Body.Title
				err = crud.Update(ctx, tx, e)
				result = e
			}
			if err != nil {
				return nil, rest.Fault(err)
			}
			return &output{Body: result}, nil
		})
	for _, tt := range []struct {
		name, title string
		valid       bool
	}{
		{"ascii-limit", strings.Repeat("a", 200), true},
		{"ascii-over", strings.Repeat("a", 201), false},
		{"unicode-limit", strings.Repeat("界", 200), true},
		{"unicode-over", strings.Repeat("界", 201), false},
		{"trim", "  short  ", true},
		{"raw-over", " " + strings.Repeat("a", 200), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := &contracts.Task{Title: tt.title}
			if err := e.Validate(t.Context()); (err == nil) != tt.valid {
				t.Errorf("entity validation = %v, valid=%t", err, tt.valid)
			}
			payload, err := json.Marshal(map[string]string{"title": tt.title})
			if err != nil {
				t.Fatal(err)
			}
			if tt.name == "ascii-limit" {
				for _, m := range api.Mounted() {
					t.Logf("mounted %s %s (%s)", m.Method, m.Path, m.Surface)
				}
				t.Logf("probe address %s", surfacesOf(api).App.Path("/probe/x"))
			}
			for _, door := range []string{"json-create", "json-update", "resource-create", "resource-update", "crud-create", "crud-update"} {
				method, at, want := http.MethodPost, surfacesOf(api).App.Path("/probe/"+door), http.StatusOK
				if door == "json-create" {
					at, want = path, http.StatusCreated
				}
				if door == "json-update" {
					method, at = http.MethodPatch, path+"/"+rowID.String()
				}
				if !tt.valid {
					want = http.StatusUnprocessableEntity
				}
				code, body := call(t, router, method, at, string(payload))
				if code != want {
					t.Errorf("%s = %d %s, want %d", door, code, body, want)
				} else if tt.valid {
					var got contracts.Task
					if err := json.Unmarshal([]byte(body), &got); err != nil || got.Title != strings.TrimSpace(tt.title) {
						t.Errorf("%s did not preserve/normalize title: %q, %v", door, got.Title, err)
					}
				}
			}
		})
	}
}
