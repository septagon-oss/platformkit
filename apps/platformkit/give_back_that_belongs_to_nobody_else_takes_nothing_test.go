package main

// A kept copy belongs to whoever kept it: the row is scoped to the tenant and owned
// by the person, and every command over it re-asks the actor's grants, the tenant
// scope and the quoted revision inside the caller's own transaction. Two people may
// therefore try the command the page drew for somebody else — a second member of the
// same tenant who holds neither of the doors over copies, and an administrator of
// another tenant who holds every door of their own — and neither attempt may take the
// copy away, because a refusal that deleted is worse than a refusal that only says no.
//
// The service is asked these questions at its own level; this asks them at the address
// a person's browser posts to, where the answer is a status, a redirect and a rendered
// list rather than an error value. The three drawn values the command needs — the
// address, the revision and the pair — are read off the keeper's own page, so nothing
// here guesses at where the mount put its routes.

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// TestAGiveBackThatBelongsToNobodyElseTakesNothingAway posts one person's drawn
// give-back as a second member of the same tenant who holds neither door over copies,
// and as an administrator of another tenant, and asks what the copy is afterwards.
// The same-tenant attempt is refused with a reason: the actor holds neither grant, so
// the command says no rather than answering with the state it did not write. The
// foreign attempt finds no copy in its own scope — a copy that is not there for a
// caller is the state that caller asked for, so it writes nothing and is not refused —
// and the test says which of the two it is by the one thing both answers have to
// agree on: the other person's copy is still on their list afterwards, and still
// theirs to give back.
func TestAGiveBackThatBelongsToNobodyElseTakesNothingAway(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, app.Options{Tenants: c.tenants, Authorize: c.auth, Entitle: c.plans,
		Authenticate: c.auth.Authenticate, Role: app.All, Transport: memory.New(), Log: quiet()})
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	title := "Repaint the loading bay"
	id := newTask(t, cfg, keeper, title, "normal")
	record := servedPage(t, cfg, keeper, acmeHost, "/app/task/tasks/"+id)
	if code, _, fault := askForm(t, cfg, keeper, acmeHost, drawnAddress(t, record, "/saved/save"), saveForm(t, record, id)); code != http.StatusSeeOther {
		t.Fatalf("keep a copy: %d (%s)", code, fault)
	}
	kept := drawnAddress(t, record, "/saved?module=")
	list := servedPage(t, cfg, keeper, acmeHost, kept)
	giveBack := drawnAddress(t, list, "/remove")
	revision := quotedRevision(t, list)

	// A member of the same tenant who may read that very task and holds neither door
	// over anybody's copies: refused, with a status that says no and no redirect that
	// would claim the copy had gone back.
	provisionAs(t, cfg, c, acmeTenant(t, cfg), "watcher@acme.localhost", "coordinator")
	watcher := signIn(t, cfg, acmeHost, "watcher@acme.localhost", adminPass)
	if code, location, fault := askForm(t, cfg, watcher, acmeHost, giveBack, "expectedRevision=1&module=task&kind=task"); code < http.StatusBadRequest || location != "" {
		t.Errorf("a stranger's POST of the drawn give-back = %d (Location %q, %s); want a refusal that claims nothing",
			code, location, fault)
	}

	// An administrator of another tenant, at its own host, holding every grant of its
	// own tenant: the copy is not there for it to see, and what it is answered with,
	// acme's row cannot be the worse for it.
	code, body := do(t, cfg, keeper, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s for globex = %d %s", tenantPath, code, body)
	}
	provision(t, cfg, uuid.MustParse(field(t, body, "id")), "root@globex.localhost")
	foreign := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, _, page := askNavigate(t, cfg, foreign, globexHost, kept); code == http.StatusOK && strings.Contains(page, title) {
		t.Error("another tenant's kept list carries a copy kept in acme")
	}
	askForm(t, cfg, foreign, globexHost, giveBack, "expectedRevision=1&module=task&kind=task")

	// Both attempts over: the copy is still its keeper's, named on their own list at
	// the revision that list drew, and still gives itself back there.
	after := servedPage(t, cfg, keeper, acmeHost, kept)
	if !strings.Contains(after, title) {
		t.Fatalf("the copy is gone from its keeper's list after attempts that owned nothing:\n%s", after)
	}
	if code, location, fault := askForm(t, cfg, keeper, acmeHost, giveBack,
		"expectedRevision="+quotedRevision(t, after)+"&module=task&kind=task"); code != http.StatusSeeOther {
		t.Errorf("the keeper's own give-back after those attempts = %d (Location %q, %s); want the 303 to the grid",
			code, location, fault)
	}
	if again := servedPage(t, cfg, keeper, acmeHost, kept); strings.Contains(again, title) {
		t.Errorf("the copy outlived its keeper's own give-back (quoted revision %s):\n%s", revision, again)
	}
}

// quotedRevision is the number the list showed beside one copy, which is the only
// number a give-back drawn by that page could quote.
func quotedRevision(t *testing.T, list string) string {
	t.Helper()
	mark := strings.Index(list, `name="expectedRevision" value="`)
	if mark < 0 {
		t.Fatal("the kept list draws no revision beside the copy it offers to give back")
	}
	rest := list[mark+len(`name="expectedRevision" value="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatal("the revision the kept list quotes is unterminated")
	}
	return rest[:end]
}
