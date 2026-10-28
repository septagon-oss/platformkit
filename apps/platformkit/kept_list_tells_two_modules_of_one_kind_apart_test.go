package main

// The kept list's filter names a module and a kind, and a kind is a word two modules
// may both use: the rows a page draws are the copies of that module's kind, not of any
// module's. Every copy this application can hold is a task's, so asking the list at a
// different module of the same kind is the one question that tells a filter reading
// both words from one reading only the kind — the copy must stay undrawn there, and be
// drawn at the module it was kept under, which is the only difference between the two
// asks this case makes.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestTheKeptListTellsTwoModulesOfOneKindApart(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	const title = "Number the loading bay notices"
	oneKeptCopy(t, cfg, keeper, title)

	if list := servedPage(t, cfg, keeper, acmeHost, "/app/keeping/saved?module=task&kind=task&offset=0"); !strings.Contains(list, title) {
		t.Fatalf("the kept list of the pair the copy was kept under shows no card for it:\n%s", keeps(list))
	}
	// The same kind, a module that kept nothing: the copy is another module's, and a
	// page asked for this one holds no card of it.
	if code, _, list := askNavigate(t, cfg, keeper, acmeHost, "/app/keeping/saved?module=media&kind=task&offset=0"); code == http.StatusOK &&
		strings.Contains(list, title) {
		t.Errorf("the kept list asked for media/task drew the copy kept under task/task; a kind is a word two modules share, and the module half of the filter decides whose copies the page is drawn from")
	}
}
