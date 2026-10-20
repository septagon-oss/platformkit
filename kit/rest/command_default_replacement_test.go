package rest_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestExplicitEmptyMapReplacesCommandDefaultThroughBothAdapters(t *testing.T) {
	type input struct {
		Values map[string]int64 `json:"values" default:"{\"rank\":7}"`
	}
	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	var received []input
	api, router, _ := mountAs(t, s, caller{}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil,
			func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in input) (*Task, error) {
				received = append(received, in)
				return &Task{Title: "Published"}, nil
			}, rest.CommandOptions{MapArgs: &rest.MapArgs{Args: map[string]crud.MapArg{
				"values": {Elem: crud.TypeInt, Keys: []entity.MapKey{{Name: "rank"}}},
			}}})
	})
	command := api.Resources()[0].Commands[0]
	httpx.Register(api.Surfaces(s.Module).App, huma.Operation{
		OperationID: "replace-default-values", Method: http.MethodPost, Path: "/replace-default-values",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		return nil, command.Run(ctx, uuid.New(), map[string]any{"values": map[string]int64{}})
	})
	for _, tc := range []struct {
		name, path, body string
	}{
		{"HTTP", "/api/v1/tasks/task/" + uuid.NewString() + "/publish", `{"values":{}}`},
		{"resource closure", "/api/v1/tasks/replace-default-values", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(received)
			if code, body := call(t, router, http.MethodPost, tc.path, tc.body); code >= 300 {
				t.Fatalf("command refused: %d %s", code, body)
			}
			if len(received) != before+1 {
				t.Fatalf("service calls = %d, want %d", len(received), before+1)
			}
			if got := received[before].Values; got == nil || len(got) != 0 {
				t.Errorf("explicit empty map reached service as %v; want a nonnil empty map", got)
			}
		})
	}
}
