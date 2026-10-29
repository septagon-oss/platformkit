package main

// The design floor leaves a page one thing to do above the fold, counted off the
// rendered fill: the programme's probe groups the visible buttons and links that share
// a filled background and refuses the page when the most-used fill appears twice. A
// record screen answers a writable person with two such controls — its own action in
// the brand fill and its delete in the danger fill, one each — and the floor holds.
//
// This case asks the same question of the state a person reaches by keeping a copy: the
// record of a row they already kept, where the give-back control stands beside the
// delete. The state is the one every person who pressed the control returns to, and the
// shell's own floor case never arrives at it, because it measures a record nobody kept.
//
// Like the probe, it reads the served document with the closed confirmation dialogs
// taken out: a native <dialog> is display:none until it is opened, so its buttons are
// not above anyone's fold.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// openTag matches a control's opening tag, so its class attribute and its label can be
// read together.
var openTag = regexp.MustCompile(`(?s)<(a|button)\b([^>]*)>(.{0,120})`)

// controlLabel is what a person reads on a control: its own words, or, where it
// carries none because they sit inside a span or a dialog asks them elsewhere, the
// attribute that names it.
func controlLabel(attrs, after string) string {
	for _, first := range []*regexp.Regexp{
		// The words a control carries, whether the component wrote them into its own
		// text region or straight between its tags.
		regexp.MustCompile(`<!--pk-text:label-->([^<]*)`),
		regexp.MustCompile(`^([^<]*)`),
	} {
		if found := first.FindStringSubmatch(after); found != nil {
			if label := strings.TrimSpace(found[1]); label != "" {
				return label
			}
		}
	}
	for _, named := range []string{`data-confirm-label="([^"]*)"`, `aria-label="([^"]*)"`, `href="([^"]*)"`} {
		if found := regexp.MustCompile(named).FindStringSubmatch(attrs); found != nil {
			return found[1]
		}
	}
	return ""
}

// fillClasses is the background fill of every control in body that carries one, by fill,
// each with the label the person reads on it. A closed dialog's controls are not in the
// count, and neither is a control drawn on a transparent ground.
func fillClasses(t *testing.T, body string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, match := range openTag.FindAllStringSubmatch(body, -1) {
		attrs, label := match[2], controlLabel(match[2], match[3])
		classes := regexp.MustCompile(`class="([^"]*)"`).FindStringSubmatch(attrs)
		if classes == nil {
			continue
		}
		fill := regexp.MustCompile(`(^|[\s:])bg-([a-z0-9][a-z0-9-]*)`).FindStringSubmatch(classes[1])
		if fill == nil || fill[2] == "transparent" {
			continue
		}
		out[fill[2]] = append(out[fill[2]], label)
	}
	return out
}

// TestTheRecordWithAKeptCopyDrawsOneFilledAction keeps a copy through the control the
// record draws, then asks what the record's own action area looks like to the person who
// comes back to it. No filled appearance may appear twice there, whichever it is: the
// record keeps one thing to do, before a copy and after one.
func TestTheRecordWithAKeptCopyDrawsOneFilledAction(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	startSaved(t, cfg, c)
	keeper := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, keeper, "Repaint the loading bay", "normal")
	at := "/app/task/tasks/" + id

	before := servedPage(t, cfg, keeper, acmeHost, at)
	save := drawnAddress(t, before, "/saved/save")
	for fill, labels := range filledIn(t, before) {
		if len(labels) > 1 {
			t.Fatalf("the record of an unkept row already draws %d controls filled %s (%v); this case is about the state that keeps them company",
				len(labels), fill, labels)
		}
	}
	if code, _, fault := askForm(t, cfg, keeper, acmeHost, save, saveForm(t, before, id)); code != http.StatusSeeOther {
		t.Fatalf("keep the copy: %d (%s), want the 303", code, fault)
	}

	fills := filledIn(t, servedPage(t, cfg, keeper, acmeHost, at))
	for fill, labels := range fills {
		if len(labels) > 1 {
			t.Errorf("the record of a row this person kept draws %d controls filled %s (%v); one thing is left to do per view",
				len(labels), fill, labels)
		}
	}
}

// filledIn is fillClasses over the page's own content, dialogs aside.
func filledIn(t *testing.T, page string) map[string][]string {
	t.Helper()
	body := regexp.MustCompile(`(?s)<dialog\b.*?</dialog>`).ReplaceAllString(page, "")
	if main := regexp.MustCompile(`(?s)<main\b.*?</main>`).FindString(body); main != "" {
		body = main
	}
	return fillClasses(t, body)
}
