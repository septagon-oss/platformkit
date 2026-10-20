package rest_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestARetainedDeckCommandRechecksItsGrantAfterRevocation(t *testing.T) {
	type input struct {
		Note string `json:"note" default:"published"`
	}
	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	grants := member{"task:read": true, "task:write": true, "task:publish": true}
	runs := 0
	api, router, admin := mountAs(t, s, grants, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil,
			func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in input) (*Task, error) {
				runs++
				row, err := crud.GetForUpdate[*Task](tx, id)
				if err != nil {
					return nil, err
				}
				row.Title = in.Note
				return row, crud.Update(ctx, tx, row, "title")
			}, rest.CommandOptions{Auth: httpx.Permission("task:publish")})
	})
	rowID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)", rowID, acme.ID, "draft"); err != nil {
		t.Fatal(err)
	}
	resource := api.Resources()[0]
	command := resource.Commands[0]
	note := ""
	httpx.Register(api.Surfaces(s.Module).App, huma.Operation{
		OperationID: "run-retained-command", Method: http.MethodPost, Path: "/run-retained-command",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if got := len(resource.CommandsFor(ctx)); (got == 1) != grants["task:publish"] {
			t.Errorf("offered %d commands with publish grant %v", got, grants["task:publish"])
		}
		return nil, rest.Fault(command.Run(ctx, rowID, map[string]any{"note": note}))
	})
	for _, tc := range []struct {
		name, note, title           string
		allowed                     bool
		status, closureStatus, runs int
	}{
		{"granted", "first", "first", true, http.StatusOK, http.StatusNoContent, 2},
		{"revoked", "forbidden", "first", false, http.StatusForbidden, http.StatusForbidden, 2},
		{"restored", "restored", "restored", true, http.StatusOK, http.StatusNoContent, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			grants["task:publish"], note = tc.allowed, tc.note
			for _, door := range []struct {
				path   string
				status int
			}{
				{"/api/v1/tasks/task/" + rowID.String() + "/publish", tc.status},
				{"/api/v1/tasks/run-retained-command", tc.closureStatus},
			} {
				code, body := call(t, router, http.MethodPost, door.path, `{"note":"`+note+`"}`)
				if code != door.status {
					t.Errorf("%s = %d %s, want %d", door.path, code, body, door.status)
				}
			}
			if runs != tc.runs {
				t.Errorf("service ran %d times, want %d", runs, tc.runs)
			}
			var title string
			if err := admin.QueryRowContext(t.Context(), "SELECT title FROM rest_tasks WHERE id = $1", rowID).Scan(&title); err != nil {
				t.Fatal(err)
			}
			if title != tc.title {
				t.Errorf("stored title = %q, want %q", title, tc.title)
			}
		})
	}
}
