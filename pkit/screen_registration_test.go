package pkit_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestChangedScreenAnchorsRefuseBeforeEffects(t *testing.T) {
	for _, changeAt := range []int{3, 4} {
		t.Run(fmt.Sprint(changeAt), func(t *testing.T) {
			cfg := onOneDatabase(t)
			calls := 0
			feature := pkit.NewModule("feature", func(*pkit.Wiring) (module.Module, error) {
				return module.Module{Name: "feature", Routes: func(s httpx.Surfaces) {
					calls++
					name := "field.title"
					if calls == changeAt {
						name = "field.changed"
					}
					v := screen.MustView(screen.ViewSpec{Ref: "app/feature/custom/editor", Cardinality: screen.One,
						Anchors: []screen.AnchorSpec{{Name: name, Control: "display", Binding: screen.Display("feature/title")}}})
					r := s.App.WithViews(v)
					op := huma.Operation{OperationID: "feature-editor", Method: "GET", Path: "/editor"}
					r.DescribePage(&op, screen.ResponseSet{Responses: []screen.Response{{View: v.Ref(), Kind: "input"}}})
					httpx.HTML(r, op, httpx.SignedIn(), func(context.Context, *struct{}) (*httpx.Page, error) {
						t.Fatal("declaration invoked the page handler")
						return nil, nil
					})
				}}, nil
			})
			rt, err := pkit.NewApp("collect").Use(doors, feature).Build(t.Context(), buildDeployment(cfg, app.All))
			if rt != nil {
				_ = rt.Close()
				t.Fatal("changed anchors returned a runtime")
			}
			if err == nil || !strings.Contains(err.Error(), "screen declarations changed") {
				t.Fatalf("refusal = %v", err)
			}
			if calls != changeAt {
				t.Fatalf("registered %d times, want %d", calls, changeAt)
			}
			if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
				t.Fatalf("refused composition migrated %d tables", got)
			}
		})
	}
}
