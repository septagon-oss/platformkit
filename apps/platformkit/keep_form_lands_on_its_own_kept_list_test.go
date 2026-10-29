package main

// Where a kept copy sends the person who pressed is not the person's to say. The one
// address a save answers with is the kept list of the pair the copy belongs to, which
// is an address this installation's own mount wrote; a submitted body carries the pair,
// the key and the version the record published and names no destination at all. So a
// body that also names a destination — a return address, an absolute one, a
// scheme-relative one, or a value carrying a header line — must be answered at this
// installation's own kept list or refused outright, and never sent elsewhere.
//
// The addresses this case asserts on are read off the record page the composition drew,
// so it checks where the mount actually lands a save rather than where a file guessed.

import (
	"net/http"
	"strings"
	"testing"
)

// TestTheKeepFormLandsOnThisInstallationsOwnKeptList presses the keep control as the
// person who may keep, once with the body the record drew and once with each body that
// additionally tries to name where the browser should go. Every accepted press lands on
// this installation's own kept list of that pair; nothing an accepted press answers
// names another host, a protocol or a header line.
func TestTheKeepFormLandsOnThisInstallationsOwnKeptList(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, keeper, "Repaint the loading bay", "normal")

	record := servedPage(t, cfg, keeper, acmeHost, "/app/task/tasks/"+id)
	save := drawnAddress(t, record, "/saved/save")
	list := drawnAddress(t, record, "/saved?module=")
	body := saveForm(t, record, id)

	for _, send := range []string{
		"", // the body the record drew, on its own
		"&back=https://evil.example/away",
		`&back=//evil.example`,
		`&back=/%5Cevil.example`, // a lone backslash reads to a browser as an authority
		"&back=" + list,
		"&module=https://evil.example",
		"&kind=task%0d%0aSet-Cookie%3Aaway%3D1",
	} {
		code, location, _ := askForm(t, cfg, keeper, acmeHost, save, body+send)
		if code != http.StatusSeeOther {
			if code < http.StatusBadRequest {
				t.Errorf("the save form answered %d to a body %q, which is neither the redirect nor a refusal", code, send)
			}
			if location != "" {
				t.Errorf("the save form refused %q with %d and still named an address to go to: %q", send, code, location)
			}
			continue
		}
		if want := list + "&offset=0"; location != want {
			t.Errorf("the save form with body %q sends the browser to %q, want this installation's own kept list %q", send, location, want)
		}
		if strings.Contains(location, "://") || strings.HasPrefix(location, "//") ||
			strings.ContainsAny(location, " \r\n\t") || strings.Contains(location, "evil") {
			t.Errorf("the redirect address %q reached by body %q is not one address of this installation", location, send)
		}
	}
}
