package pkit_test

import (
	"context"
	"github.com/septagon-oss/platformkit/ui/screens"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/pkit"
)

type ReviewItem struct {
	crud.Base
	Title string `json:"title"`
}

func (*ReviewItem) TableName() string { return "review_items" }

func TestBuildRefusesAnUnknownDeckCommandBeforeMigrating(t *testing.T) {
	cfg := onOneDatabase(t)
	review := pkit.NewModule("review", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "review", Routes: func(r httpx.Surfaces) {
			rest.Spec[*ReviewItem]{Module: "review", Entity: "item", Path: "/items", ReadAuth: httpx.SignedIn(), WriteAuth: httpx.SignedIn(), Operations: []httpx.CRUD{rest.List, rest.Read}, Present: entity.EntryHints{
				Sortable: []string{"title"}, Deck: &entity.DeckHints{Order: "title", Directions: map[string]entity.DeckDirection{"right": {Command: "vanish", Label: "File", Tone: "neutral", Icon: "folder"}}},
			}}.Mount(r)
		}}, nil
	})
	rt, err := pkit.NewApp("collect").Use(doors, desk, review).WorkspaceCatalog(app.WorkspaceCatalogRoute(func(ctx context.Context, resources []httpx.Resource) (*screens.Catalog, error) {
		c := screens.Describe(ctx, resources)
		return &c, nil
	})).Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		rt.Close()
		t.Fatal("invalid deck opened a runtime")
	}
	says(t, err, "review/item deck.directions.right.command")
	says(t, err, "vanish")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Fatalf("invalid deck migrated %d tables", got)
	}
}
