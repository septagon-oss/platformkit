package main

// The design floor leaves a page one thing to do above the fold, counted off the
// rendered fill: the probe groups the visible buttons and links that share a filled
// background and refuses the page when the most-used fill appears twice. A record
// screen answers a person with its own action in the brand fill and its delete in the
// danger fill, one each, and its pressed keep control is drawn quiet beside them.
//
// The list of kept copies is the other page this composition creates, and its content
// grows with use: every copy a person keeps adds a card, and every card carries that
// copy's own give-back. So the page's own actions are not a fixed number, and the
// state a person reaches the second time they keep something is not the state that was
// measured. This case asks the fill question of that list with two copies on it, once
// unfiltered and once filtered to one pair, and then — when the programme's gate is
// named in the environment — asks a browser the same question over the served page.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
)

// TestTheKeptListOfTwoCopiesDrawsOneFilledAction keeps two copies through the control
// the record draws, then counts what the kept list offers its keeper to do, per filled
// appearance. No fill may appear twice on that page, whichever copy it belongs to: a
// list of one person's own copies is not a page of equal primary actions, and the
// number of its cards is whatever the person happened to keep.
func TestTheKeptListOfTwoCopiesDrawsOneFilledAction(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	list := ""
	for _, title := range []string{"Repaint the loading bay", "Rebook the Zurich trip"} {
		id := newTask(t, cfg, keeper, title, "normal")
		record := servedPage(t, cfg, keeper, acmeHost, "/app/task/tasks/"+id)
		if code, _, fault := askForm(t, cfg, keeper, acmeHost, drawnAddress(t, record, "/saved/save"), saveForm(t, record, id)); code != http.StatusSeeOther {
			t.Fatalf("keep a copy of %q: %d (%s)", title, code, fault)
		}
		if list == "" {
			// The filtered list, read off the record's own link, plus the same page
			// with its filter dropped: both draw the cards, and a person arrives at
			// each of them from a different control.
			list = drawnAddress(t, record, "/saved?module=")
		}
	}
	for _, addr := range []string{list, list[:strings.Index(list, "?")]} {
		page := servedPage(t, cfg, keeper, acmeHost, addr)
		if !strings.Contains(page, "Repaint the loading bay") || !strings.Contains(page, "Rebook the Zurich trip") {
			t.Fatalf("the kept list at %s does not show both copies this person kept:\n%s", addr, page)
		}
		for fill, labels := range filledIn(t, page) {
			if len(labels) > 1 {
				t.Errorf("the kept list at %s draws %d controls filled %s (%v); one thing is left to do per view",
					addr, len(labels), fill, labels)
			}
		}
	}
	serveKeptListToTheExternalGate(t, cfg, keeper, list, list[:strings.Index(list, "?")])
}

// serveKeptListToTheExternalGate answers the kept list behind the same cookie-carrying
// proxy the shell's own floor case builds, and asks the programme's design gate what it
// makes of the page at a phone and a desktop. It answers nothing when no gate is named:
// the markup count above stands on its own, and the browser is the same question asked
// of computed style rather than of class names.
func serveKeptListToTheExternalGate(t *testing.T, cfg config.Config, keeper *http.Client, addresses ...string) {
	t.Helper()
	gate := os.Getenv("PLATFORMKIT_SHELL_DESIGN_GATE")
	if gate == "" {
		return
	}
	target, err := url.Parse("http://" + cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	origin := *target
	origin.Host = acmeHost
	proxy := httputil.NewSingleHostReverseProxy(target)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		direct(r)
		r.Host = acmeHost
		for _, cookie := range keeper.Jar.Cookies(&origin) {
			r.AddCookie(cookie)
		}
	}
	preview := httptest.NewServer(proxy)
	t.Cleanup(preview.Close)
	work, err := os.MkdirTemp("/tmp", "pkit-kept-floor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	for _, addr := range addresses {
		cmd := exec.CommandContext(ctx, "python3", gate, preview.URL+addr, "--widths", "390,1440")
		cmd.Env = append(os.Environ(), "TMPDIR="+work)
		output, err := cmd.CombinedOutput()
		t.Logf("python3 %s %s%s --widths 390,1440\n%s", gate, preview.URL, addr, output)
		if err != nil {
			t.Errorf("the external design floor refuses the kept list %s: %v", addr, err)
		}
	}
}
