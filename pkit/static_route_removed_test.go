package pkit_test

// The mirror of the added tree. A file tree the gated registration mounted and the
// built one dropped is the same disagreement seen from the other side: what serves
// is not what the gates read, and the address that lost its tree is a stylesheet
// every page of the composition already links. Build refuses it, and refuses it
// with the schema still empty, exactly as it refuses a tree that appears.

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBuildRefusesAStaticTreeRemovedAfterDeclarations(t *testing.T) {
	cfg := onOneDatabase(t)
	registrations := 0
	assets := pkit.NewModule("assets", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "assets", Routes: func(s httpx.Surfaces) {
			registrations++
			if registrations == 1 {
				s.Public.Static("/files", fstest.MapFS{
					"sheet.css": &fstest.MapFile{Data: []byte("body { color: rebeccapurple }")},
				})
			}
		}}, nil
	})

	rt, err := pkit.NewApp("collect").Use(doors, desk, assets).Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		defer rt.Close()
		t.Error("Build returned a runtime for a composition that stopped mounting its asset tree")
		req := httptest.NewRequest(http.MethodGet, "http://collect.test/assets/files/sheet.css", nil)
		response := httptest.NewRecorder()
		rt.Handler().ServeHTTP(response, req)
		t.Logf("the address the gates read a tree at answers: %d %q", response.Code, response.Body.String())
	}
	if err == nil {
		t.Fatal("Build accepted a composition that mounted its asset tree when it was gated and not when it was built")
	}
	says(t, err, "/assets/files/*")
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Build migrated %d tables before refusing the removed static mount", got)
	}
}
