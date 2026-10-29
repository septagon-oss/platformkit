package main

// The design floor allows one filled primary action above the fold: "one thing to do
// per view" (MAX_PRIMARY_CTA in the programme's own gate). A record screen already
// answers a writable person with its one Edit action, drawn in the design system's
// default appearance, so anything else added to that toolbar is drawn as something
// else — a link, an outline, a quiet control — rather than as a second filled button.
//
// This case reads the served document rather than a screenshot: the two controls that
// share the page's one filled appearance are visible in their own class attribute, and
// the browser-measured floor in the shell's own case agrees with what is counted here.
//
// It checks both states of the keep control, because the person who has already kept
// a copy is the person the pressed button is drawn for.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// actionClasses is the class attribute of the control on the page that leads to the
// given address, and the count of controls that carry exactly that appearance.
func actionClasses(t *testing.T, page, href string) (string, int) {
	t.Helper()
	tag := regexp.MustCompile(`(?s)<(?:a|button)\b[^>]*>`)
	quoted := regexp.QuoteMeta(href)
	want := ""
	count := 0
	for _, open := range tag.FindAllString(page, -1) {
		if !regexp.MustCompile(`href="` + quoted + `"`).MatchString(open) {
			continue
		}
		classes := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(open)
		if classes == nil {
			t.Fatalf("the control leading to %s carries no class: %s", href, open)
		}
		want = classes[1]
	}
	if want == "" {
		t.Fatalf("the page offers no control leading to %s", href)
	}
	for _, open := range tag.FindAllString(page, -1) {
		classes := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(open)
		if classes != nil && classes[1] == want {
			count++
		}
	}
	return want, count
}

// TestTheKeepControlSharesTheRecordScreenWithOnePrimaryAction asks the record screen
// who is given the record's one filled primary appearance once the keep-a-copy control
// stands beside it. The answer must stay the record's own action: a person who may
// both edit and keep a record is still shown one thing to do.
func TestTheKeepControlSharesTheRecordScreenWithOnePrimaryAction(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, keeper, "Repaint the loading bay", "normal")
	record := "/app/task/tasks/" + id

	unpressed := servedPage(t, cfg, keeper, acmeHost, record)
	save := drawnAddress(t, unpressed, "/saved/save")
	if !strings.Contains(unpressed, `<form`) || !strings.Contains(unpressed, save) {
		t.Fatalf("the record draws no form posting to %s", save)
	}
	primary, count := actionClasses(t, unpressed, record+"/edit")
	if count != 1 {
		t.Errorf("the record before a copy is kept draws %d controls in its one filled appearance %q; the floor allows one thing to do per view",
			count, primary)
	}

	if code, _, fault := askForm(t, cfg, keeper, acmeHost, save, saveForm(t, unpressed, id)); code != http.StatusSeeOther {
		t.Fatalf("keep the copy: %d (%s), want the 303 back to the record", code, fault)
	}
	if _, count := actionClasses(t, servedPage(t, cfg, keeper, acmeHost, record), record+"/edit"); count != 1 {
		t.Errorf("the record after a copy is kept draws %d controls in its one filled appearance; the floor allows one thing to do per view", count)
	}
}
