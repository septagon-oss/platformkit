package main

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/config"
)

// The saved surface is one capability read in two places: the control on the record a
// person is reading, and the list of the copies they have already kept. Both are the
// keeper's own and of nobody else's — a copy is private to whoever kept it — so this
// boots the reference composition, keeps a copy of one of its tasks through the form
// the record screen itself draws, and then asks a second person of the same tenant
// and the administrator of another tenant what either of them can see.
//
// The control is read the way a browser reads it: the pressed state is an attribute
// of the button the page was rendered with, and the button's name does not change
// with it. That is the only form of this claim that a screen reader and a golden file
// can both check.
func TestAPersonSeesTheHeartAndTheirKeptCopiesAndNobodyElseDoes(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	// The tenant's own administrator keeps the copy: no persona this application
	// seeds is granted the two doors of a person's copies, and the administrator's
	// role is the one that holds every tenant-scoped grant the composition defines,
	// so it is the person a case can press the control as. A coordinator may read
	// the very same task and keeps nothing — which is the second half of what this
	// case checks.
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	title := "Repaint the loading bay"
	id := newTask(t, cfg, keeper, title, "normal")
	provisionAs(t, cfg, c, acmeTenant(t, cfg), "watcher@acme.localhost", "coordinator")
	watcher := signIn(t, cfg, acmeHost, "watcher@acme.localhost", adminPass)
	record := "/app/task/tasks/" + id

	// The control before anyone has kept anything: unpressed, and posted with the
	// key and the version the record itself published.
	page := servedPage(t, cfg, keeper, acmeHost, record)
	if !strings.Contains(page, `aria-pressed="false"`) {
		t.Fatalf("the record of a task offers no save control to the person who may read it:\n%s", keeps(page))
	}
	// The addresses come off the page rather than out of this file: the claim is
	// that the drawn control posts to a route that answers, and only the mount
	// knows where it mounted.
	save := drawnAddress(t, page, "/saved/save")
	kept := drawnAddress(t, page, "/saved?module=")
	form := saveForm(t, page, id)

	// Keeping it through that form answers 303 back to the record, so what the
	// person reads after pressing is a re-read rather than the state they asked for.
	code, location, fault := askForm(t, cfg, keeper, acmeHost, save, form)
	if code != http.StatusSeeOther {
		t.Fatalf("POST the save form = %d (%s), want the 303 back to the record", code, fault)
	}
	// Where it lands is the kept list of this pair: the person who pressed wanted
	// the copy, and the grid is the page that shows what they now hold. The record
	// is read again below, so the pressed state is checked whatever the redirect
	// answered.
	if !strings.HasSuffix(location, kept+"&offset=0") {
		t.Errorf("the save form redirects to %q, want the kept list it is a copy of (%q)", location, kept)
	}
	page = servedPage(t, cfg, keeper, acmeHost, record)
	if !strings.Contains(page, `aria-pressed="true"`) {
		t.Errorf("after the save the control is not pressed: %s", keeps(page))
	}
	if !strings.Contains(page, "Saved to your tasks") {
		t.Error("a pressed control carries no word saying that a copy exists")
	}

	// The kept list names the copy it holds, and the way to give it back.
	list := servedPage(t, cfg, keeper, acmeHost, kept)
	if !strings.Contains(list, title) {
		t.Fatalf("the kept list shows no card for the copy just kept:\n%s", list)
	}
	giveBack := drawnAddress(t, list, "/remove")

	// A second person of the same tenant, a coordinator who may read that very task
	// and holds neither door over copies, is shown neither the pressed state nor the
	// kept card: what the list holds is their own, and their own record page says
	// nothing about a copy somebody else kept.
	if other := servedPage(t, cfg, watcher, acmeHost, record); strings.Contains(other, `aria-pressed="true"`) ||
		strings.Contains(other, "Saved to your tasks") {
		t.Error("a person who kept nothing is shown another person's pressed control")
	}
	if code, _, other := askNavigate(t, cfg, watcher, acmeHost, kept); code == http.StatusOK {
		t.Errorf("the kept list %s is answered to a person who holds neither of its two grants (%d):\n%s",
			kept, code, keeps(other))
	}

	// Another tenant, at its own host: the same addresses, and nothing of this one.
	code, body := do(t, cfg, keeper, http.MethodPost, acmeHost, tenantPath,
		`{"slug":"globex","name":"Globex","host":"`+globexHost+`"}`)
	if code != http.StatusCreated {
		t.Fatalf("POST %s for globex = %d %s, want the tenant created", tenantPath, code, body)
	}
	globexID := uuid.MustParse(field(t, body, "id"))
	provision(t, cfg, globexID, "root@globex.localhost")
	other := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)
	if code, _, page := askNavigate(t, cfg, other, globexHost, kept); code == http.StatusOK &&
		strings.Contains(page, title) {
		t.Error("another tenant's kept list carries a copy kept in acme")
	}

	// The copy is the keeper's own to give back: the card's form quotes the revision
	// it showed, and once it is given the list holds nothing.
	if code, _, fault = askForm(t, cfg, keeper, acmeHost, giveBack,
		"expectedRevision=1&module=task&kind=task"); code != http.StatusSeeOther {
		t.Fatalf("POST the removal = %d (%s), want the 303 back to the grid", code, fault)
	}
	if again := servedPage(t, cfg, keeper, acmeHost, kept); strings.Contains(again, title) {
		t.Error("the copy is still on the list after it was given back")
	}
}

// drawnAddress is one address as the page drew it, matched on the suffix that says
// which control it belongs to. A page that draws a control posting nowhere, or one
// that posts to a path no route answers at, fails here rather than at a guess this
// file made about where a mount put its routes.
func drawnAddress(t *testing.T, page, suffix string) string {
	t.Helper()
	at := regexp.MustCompile(`(?:action|href)="([^"]*` + regexp.QuoteMeta(suffix) + `[^"]*)"`).FindStringSubmatch(page)
	if at == nil {
		t.Fatalf("no drawn address ending %q on the page:\n%s", suffix, keeps(page))
	}
	// The page escapes the ampersands of its own query strings, and the address a
	// browser follows or posts to is the unescaped one.
	return html.UnescapeString(at[1])
}

// servedPage is one workspace page as this person's browser is answered with it.
func servedPage(t *testing.T, cfg config.Config, client *http.Client, host, path string) string {
	t.Helper()
	code, _, body := askNavigate(t, cfg, client, host, path)
	if code != http.StatusOK {
		t.Fatalf("GET %s as a signed-in person = %d, want the page", path, code)
	}
	return body
}

// saveForm is the body of the form the record screen drew for this row — the pair,
// the key and the version are taken from what the page shows rather than written out
// here, because the claim under test is that the drawn control posts what the record
// published.
func saveForm(t *testing.T, page, key string) string {
	t.Helper()
	fields := map[string]string{"module": "task", "kind": "task", "key": key}
	for _, name := range []string{"module", "kind", "key", "expectedSourceVersion"} {
		value := regexp.MustCompile(`name="` + name + `" value="([^"]*)"`).FindStringSubmatch(page)
		if value == nil {
			t.Fatalf("the save form posts no %q field: %s", name, keeps(page))
		}
		if want, named := fields[name]; named && want != value[1] {
			t.Fatalf("the save form posts %s=%q, want %q", name, value[1], want)
		}
		fields[name] = value[1]
	}
	return "module=task&kind=task&key=" + fields["key"] + "&expectedSourceVersion=" + fields["expectedSourceVersion"]
}

// keeps is the part of a page a save control lives on, so a failure prints the
// control and not the whole document.
func keeps(page string) string {
	var out []string
	for _, line := range strings.Split(page, "\n") {
		if strings.Contains(line, "aria-pressed") || strings.Contains(line, "/app/saved") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return strings.Join(out, "\n")
}
