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

func TestDeckCommandClosureRefusesTheSameMissingGrantAsItsRoute(t *testing.T) {
	type input struct {
		Note string `json:"note" default:"published"`
	}
	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	runs := 0
	api, router, admin := mountAs(t, s, denies{permission: "task:publish"}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil,
			func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in input) (*Task, error) {
				runs++
				row, err := crud.Get[*Task](tx, id)
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
	httpx.Register(api.Surfaces(s.Module).App, huma.Operation{
		OperationID: "invoke-deck-command", Method: http.MethodPost, Path: "/invoke-deck-command",
	}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		if len(resource.CommandsFor(ctx)) != 0 {
			t.Error("caller without task:publish was offered the command")
		}
		return nil, rest.Fault(command.Run(ctx, rowID, nil))
	})
	for _, path := range []string{
		"/api/v1/tasks/task/" + rowID.String() + "/publish",
		"/api/v1/tasks/invoke-deck-command",
	} {
		if code, body := call(t, router, http.MethodPost, path, ""); code != http.StatusForbidden {
			t.Errorf("%s = %d %s; want 403 for the missing command grant", path, code, body)
		}
	}
	if runs != 0 {
		t.Errorf("command service ran %d times without its grant", runs)
	}
	var title string
	if err := admin.QueryRowContext(t.Context(), "SELECT title FROM rest_tasks WHERE id = $1", rowID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "draft" {
		t.Errorf("denied command committed title %q", title)
	}
}
