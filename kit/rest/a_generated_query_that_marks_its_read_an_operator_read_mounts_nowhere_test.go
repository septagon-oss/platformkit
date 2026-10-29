package rest_test

// A generated query is mounted only under one workspace read guard, and the mount site is
// where that is decided. Two declarations reach that rule through one clause each, with no
// other clause to fall back on: a Spec that marks its read an operator read while its
// ReadAuth holds an honest workspace permission, and a Spec whose ReadAuth is an operator
// permission outright. Asked apart from the read they belong to, the first is a tenant read
// carrying a label the registration would publish, and the second is a member-facing
// address only an operator could ever answer. Each is asked here for the refusal that names
// the rule, and for the module it must leave untouched — no resource registered, no
// operation recorded, no address mounted. The same fixture without the operator marking is
// asked last, so a refusal is seen to come from the marking and not from the shape.

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

func TestAGeneratedQueryThatMarksItsReadAnOperatorReadMountsNowhere(t *testing.T) {
	newQuery := func(entityName, path string) rest.Spec[entity.Entity] {
		s := rest.ReadForm(func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in queryInputs) (queryResult, error) {
			return queryResult{in.Title, in.Rank}, nil
		})
		s.Module, s.Entity, s.Path = "query", entityName, path
		s.ReadAuth = httpx.Permission("query:read")
		return s
	}

	for _, side := range []struct {
		name    string
		entity  string
		path    string
		broaden func(s *rest.Spec[entity.Entity])
	}{
		{"the read is marked an operator read", "ledger", "/ledgers", func(s *rest.Spec[entity.Entity]) { s.OperatorRead = true }},
		{"the guard is an operator permission", "audit", "/audits", func(s *rest.Spec[entity.Entity]) { s.ReadAuth = httpx.OperatorPermission("query:read") }},
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
			if sentence := "generated queries require one workspace read guard"; !strings.Contains(refused, sentence) {
				t.Fatalf("the mount of %s %q answered %q, want the refusal that names %q", side.entity, side.path, refused, sentence)
			}
			for _, res := range api.Resources() {
				if res.Entity == side.entity {
					t.Errorf("the refused mount registered %s.%s, want the resource left unregistered", res.Module, res.Entity)
				}
			}
			for _, op := range api.Recorded() {
				if strings.HasPrefix(op.OperationID, "query-"+side.entity) {
					t.Errorf("the refused mount recorded %s, want no operation of its own", op.OperationID)
				}
			}
			if code, body := call(t, router, http.MethodPost, "/api/v1/query"+side.path, `{"title":"Notebook"}`); code != http.StatusNotFound {
				t.Errorf("POST the refused form's address answered %d %s, want %d: a mount that refused opens no query", code, body, http.StatusNotFound)
			}
		})
	}

	_, router, _ := mountAs(t, newQuery("answer", "/answers"), member{"query:read": true})
	if code, body := call(t, router, http.MethodPost, "/api/v1/query/answers", `{"title":"Notebook"}`); code != http.StatusOK || !strings.Contains(body, "Notebook") {
		t.Fatalf("the same fixture with one workspace read guard answered %d %s, want 200 with its own answer", code, body)
	}
}
