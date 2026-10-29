package rest_test

// A generated query is one read declaration asked twice: the mount hands the guard to
// the registered resource and to the address the same form mounts beside it. What keeps
// that pair from drifting is the mount's own rule — a generated query is mounted only
// under one workspace read guard — so the rule is asked here for the declarations it
// exists to turn away: no guard stated, a public guard, an operator guard, and one guard
// stated in both fields. Each is refused at the mount site with the sentence that names
// it, and leaves the module with no resource, no recorded operation and no address. The
// honest declaration is asked in the same shape, so the refusal is seen to come from the
// guard and not from the fixture.

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func TestAGeneratedQueryMountsOnlyUnderTheOneWorkspaceReadGuardItDeclares(t *testing.T) {
	newQuery := func(entityName, path string) rest.Spec[entity.Entity] {
		s := rest.ReadForm(func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in queryInputs) (queryResult, error) {
			return queryResult{in.Title, in.Rank}, nil
		})
		s.Module, s.Entity, s.Path = "query", entityName, path
		s.ReadAuth = httpx.Permission("query:read")
		return s
	}

	for _, side := range []struct {
		name     string
		entity   string
		path     string
		broaden  func(s *rest.Spec[entity.Entity])
		sentence string
	}{
		{"no read guard stated", "listing", "/listings", func(s *rest.Spec[entity.Entity]) { s.ReadAuth = httpx.Auth{} }, "generated queries require one workspace read guard"},
		{"a public read guard", "notice", "/notices", func(s *rest.Spec[entity.Entity]) { s.ReadAuth = httpx.Public() }, "generated queries require one workspace read guard"},
		{"an operator read guard", "ledger", "/ledger", func(s *rest.Spec[entity.Entity]) { s.ReadAuth, s.OperatorRead = httpx.Auth{}, true }, "generated queries require one workspace read guard"},
		{"one guard stated in both fields", "search", "/searches", func(s *rest.Spec[entity.Entity]) { s.Read, s.ReadAuth = "query:read", httpx.Permission("query:read") }, "generated queries require one workspace read guard"},
		{"one workspace read guard, which mounts", "answer", "/answers", func(*rest.Spec[entity.Entity]) {}, ""},
	} {
		t.Run(side.name, func(t *testing.T) {
			mounted := newQuery(side.entity, side.path)
			side.broaden(&mounted)
			var refused string
			api, router, _ := mountAs(t, newQuery("query", "/queries"), member{"query:read": true}, func(api *httpx.API) {
				defer func() {
					if got := recover(); got != nil {
						refused = fmt.Sprint(got)
					}
				}()
				mounted.Mount(api.Surfaces("query"))
			})

			if side.sentence != "" {
				if refused == "" {
					t.Fatalf("the mount of %s %q answered no refusal, want it to refuse %q", side.entity, side.path, side.sentence)
				}
				if !strings.Contains(refused, side.sentence) {
					t.Errorf("the mount site says %q, want it to name %q", refused, side.sentence)
				}
				for _, op := range api.Recorded() {
					if strings.HasPrefix(op.OperationID, "query-"+side.entity) {
						t.Errorf("the refused mount recorded %s, want no operation of its own", op.OperationID)
					}
				}
				for _, res := range api.Resources() {
					if res.Entity == side.entity {
						t.Errorf("the refused mount registered %s.%s, want the resource left unregistered", res.Module, res.Entity)
					}
				}
				if code, body := call(t, router, http.MethodPost, "/api/v1/query"+side.path, `{"title":"Notebook"}`); code != http.StatusNotFound {
					t.Errorf("POST the refused form's address answered %d %s, want %d: a mount that refused opens no query", code, body, http.StatusNotFound)
				}
				return
			}

			if refused != "" {
				t.Fatalf("the mount of one workspace read guard refused: %s", refused)
			}
			var registered bool
			for _, res := range api.Resources() {
				if res.Entity == side.entity {
					registered = true
				}
			}
			if !registered {
				t.Error("the honest declaration registered no query resource, so no case above turned on the guard")
			}
			code, body := call(t, router, http.MethodPost, "/api/v1/query"+side.path, `{"title":"Notebook"}`)
			if code != http.StatusOK || !strings.Contains(body, "Notebook") {
				t.Errorf("the honest declaration's query answered %d %s, want 200 with its own answer", code, body)
			}
		})
	}
}
