package pkit_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBuildRefusesAStaticTreeAddedAfterDeclarations(t *testing.T) {
	cfg := onOneDatabase(t)
	registrations := 0
	assets := pkit.NewModule("assets", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "assets", Routes: func(s httpx.Surfaces) {
			registrations++
			if registrations > 1 {
				s.Public.Static("/files", fstest.MapFS{
					"private.txt": &fstest.MapFile{Data: []byte("private fixture")},
				})
			}
		}}, nil
	})

	rt, err := pkit.NewApp("collect").Use(doors, desk, assets).Build(t.Context(), buildDeployment(cfg, app.All))
	if rt != nil {
		defer rt.Close()
		t.Error("Build returned a runtime for a static tree absent from its first registration")
		req := httptest.NewRequest(http.MethodGet, "http://collect.test/assets/files/private.txt", nil)
		response := httptest.NewRecorder()
		rt.Handler().ServeHTTP(response, req)
		if response.Code == http.StatusOK && strings.Contains(response.Body.String(), "private fixture") {
			t.Logf("a tree absent from the dry registration was served publicly: %s %s", req.URL.Path, response.Body.String())
		}
	}
	if err == nil {
		t.Error("Build accepted a public static tree absent from its first registration")
	}
	if got := tablesIn(t, cfg.Database.MigrateURL); got != 0 {
		t.Errorf("Build migrated %d tables before refusing the changed static mount", got)
	}
}
