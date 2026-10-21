package rest_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestServiceSourceOwnsJSONAndResourceReadsAndCounts(t *testing.T) {
	id := uuid.New()
	s := spec
	s.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
	s.Source = &rest.ReadSource[*Task]{Get: func(context.Context, db.Tx[db.Tenant], uuid.UUID) (*Task, error) {
		return &Task{Base: crud.Base{ID: id}, Title: "Scoped result"}, nil
	},
		List: func(_ context.Context, _ db.Tx[db.Tenant], q crud.Query) ([]*Task, int64, error) {
			return []*Task{{Base: crud.Base{ID: id}, Title: "Scoped result"}}, 17, nil
		}}
	_, handler, _ := mountAs(t, s, caller{}, func(api *httpx.API) {
		r := api.Resources()[0]
		httpx.Register(api.Surfaces(s.Module).App, huma.Operation{OperationID: "source-count", Method: http.MethodGet, Path: "/source-count"}, httpx.SignedIn(),
			func(ctx context.Context, _ *struct{}) (*struct{ Body int64 }, error) {
				count, err := r.Count(ctx)
				return &struct{ Body int64 }{count}, err
			})
		httpx.Register(api.Surfaces(s.Module).App, huma.Operation{OperationID: "source-get", Method: http.MethodGet, Path: "/source-get"}, httpx.SignedIn(),
			func(ctx context.Context, _ *struct{}) (*struct{ Body map[string]any }, error) {
				row, err := r.Get(ctx, id)
				return &struct{ Body map[string]any }{row}, err
			})
	})
	for _, path := range []string{"/api/v1/tasks/task", "/api/v1/tasks/task/" + id.String(), "/api/v1/tasks/source-get"} {
		code, body := call(t, handler, http.MethodGet, path, "")
		if code != http.StatusOK || !strings.Contains(body, "Scoped result") {
			t.Fatalf("%s: %d %s", path, code, body)
		}
	}
	if code, body := call(t, handler, http.MethodGet, "/api/v1/tasks/source-count", ""); code != http.StatusOK || strings.TrimSpace(body) != "17" {
		t.Fatalf("count: %d %s", code, body)
	}
}

func TestResultCommandKeepsAuthoritativeReplyThroughBothAdapters(t *testing.T) {
	type in struct {
		IfRevision int64 `json:"ifRevision"`
	}
	type result struct {
		Revision int64 `json:"revision"`
	}
	_, handler, _ := mountAs(t, spec, caller{}, func(api *httpx.API) {
		rest.CommandResult(api.Surfaces(spec.Module), spec, "cast", "Cast", "", nil,
			func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, body in) (rest.Response[result], error) {
				return rest.Response[result]{Result: result{body.IfRevision + 1}, UndoToken: "opaque"}, nil
			}, rest.CommandOptions{})
		command := api.Resources()[0].Commands[0]
		httpx.Register(api.Surfaces(spec.Module).App, huma.Operation{OperationID: "result-closure", Method: http.MethodPost, Path: "/result-closure"}, httpx.SignedIn(),
			func(ctx context.Context, _ *struct{}) (*struct{ Body httpx.CommandReply }, error) {
				out, err := command.Execute(ctx, uuid.New(), map[string]any{"ifRevision": int64(4)})
				return &struct{ Body httpx.CommandReply }{out}, err
			})
	})
	for _, path := range []string{"/api/v1/tasks/task/" + uuid.NewString() + "/cast", "/api/v1/tasks/result-closure"} {
		code, body := call(t, handler, http.MethodPost, path, `{"ifRevision":4}`)
		if code != http.StatusOK || !strings.Contains(body, `"revision":5`) || !strings.Contains(body, `"undoToken":"opaque"`) {
			t.Fatalf("reply: %d %s", code, body)
		}
	}
}
