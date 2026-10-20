package rest_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// A client builds its deck by handing the served queue and order back to the
// list route as ?filter= and ?sort=. Each tenant must get exactly its own
// queued rows, in deck order, and never a row of the other tenant.
func TestTheServedDeckQueueSelectsOnlyTheTenantsQueuedRowsInOrder(t *testing.T) {
	const acmeHost, globexHost = "acme.test", "globex.test"
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		tenant        uuid.UUID
		title, status string
	}{
		{acme.ID, "acme b", "open"}, {acme.ID, "acme a", "open"}, {acme.ID, "acme c", "done"},
		{globex.ID, "0 globex", "open"}, {globex.ID, "globex done", "done"},
	} {
		if _, err := admin.ExecContext(t.Context(),
			"INSERT INTO rest_tasks (id, tenant_id, title, status) VALUES ($1, $2, $3, $4)",
			uuid.New(), row.tenant, row.title, row.status); err != nil {
			t.Fatal(err)
		}
	}
	s := deckSpec()
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: acmeHost, Installation: acmeHost,
		Tenants:   hosts{byHost: map[string]tenancy.Tenant{acmeHost: acme, globexHost: globex}},
		Conn:      app,
		Authorize: member{"task:read": true, "task:write": true, "task:publish": true, "task:archive": true},
		Entitle:   everyPlan{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s.Mount(api.Surfaces(s.Module))
	run := func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Task, error) { return nil, nil }
	for _, verb := range []string{"publish", "archive"} {
		rest.Command(api.Surfaces(s.Module), s, verb, verb, "", nil, run, rest.CommandOptions{Auth: httpx.Permission("task:" + verb)})
	}
	httpx.Register(api.Surfaces(s.Module).App, huma.Operation{OperationID: "deck-catalog", Method: http.MethodGet, Path: "/deck-catalog"}, httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{ Body screens.Catalog }, error) {
		return &struct{ Body screens.Catalog }{screens.Describe(ctx, api.Resources())}, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}

	for at, want := range map[string][]string{acmeHost: {"acme a", "acme b"}, globexHost: {"0 globex"}} {
		t.Run(at, func(t *testing.T) {
			code, body := callHost(t, router, http.MethodGet, at, "/api/v1/tasks/deck-catalog", "")
			if code != http.StatusOK {
				t.Fatalf("catalogue = %d %s", code, body)
			}
			var catalog screens.Catalog
			if err := json.Unmarshal([]byte(body), &catalog); err != nil {
				t.Fatal(err)
			}
			var deck *screens.DeckPresentation
			for _, e := range catalog.Resources {
				if e.Presentation != nil && e.Presentation.Deck != nil {
					deck = e.Presentation.Deck
				}
			}
			if deck == nil || len(deck.Queue) == 0 || deck.Order == "" {
				t.Fatalf("no queued, ordered deck served: %s", body)
			}
			query := url.Values{"sort": {deck.Order}, "filter": deck.Queue}
			code, body = callHost(t, router, http.MethodGet, at, "/api/v1/tasks/task?"+query.Encode(), "")
			if code != http.StatusOK {
				t.Fatalf("list with the deck's queue %v and order %q = %d %s", deck.Queue, deck.Order, code, body)
			}
			var page struct {
				Items []struct {
					Title string `json:"title"`
				} `json:"items"`
			}
			if err := json.Unmarshal([]byte(body), &page); err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range page.Items {
				got = append(got, item.Title)
			}
			if !slices.Equal(got, want) {
				t.Errorf("deck rows at %s = %q, want %q", at, got, want)
			}
		})
	}
}
