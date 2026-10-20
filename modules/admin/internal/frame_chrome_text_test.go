package internal

// frame_chrome_text_test.go is the frame's own size invariant, in the fake the
// package already owns: the chrome — everything the shell draws outside <main>
// — is one body step. The design floor allows a page two body sizes in total,
// and the three chrome lines used to take three of them between them, which is
// the whole of a generated page's body-size set.
//
// The browser measures computed font-size; this measures the step each Text
// carries, which is the same decision one level before CSS. No database, no
// locale, no browser: the frame renders from a page.Request.

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
)

// chromeParagraphs returns every <p> the frame draws outside <main>, as
// `data-size` plus its text. The chrome is found structurally — the header and
// footer are main's siblings — for the same reason the browser probe finds it
// that way: no data-* attribute was invented for a region the shell already
// knows about.
func chromeParagraphs(t *testing.T, markup string) []string {
	t.Helper()
	begin := strings.Index(markup, "<main")
	if begin < 0 {
		t.Fatalf("the frame drew no <main>:\n%s", markup)
	}
	end := strings.Index(markup[begin:], "</main>")
	if end < 0 {
		t.Fatalf("<main> was never closed:\n%s", markup)
	}
	outside := markup[:begin] + markup[begin+end:]
	var out []string
	for _, element := range regexp.MustCompile(`<p [^>]*>.*?</p>`).FindAllString(outside, -1) {
		// Only the paragraphs the Text component drew: the frame also mounts a
		// confirm dialog outside <main>, whose empty message <p> carries neither
		// data-element nor data-size, and is not the frame's own copy.
		if !strings.Contains(element, `data-element="p"`) {
			continue
		}
		size := regexp.MustCompile(`data-size="([^"]*)"`).FindStringSubmatch(element)
		if size == nil {
			t.Errorf("a chrome <p> carries no data-size: %s", element)
			continue
		}
		out = append(out, size[1]+"\x00"+regexp.MustCompile(`<[^>]*>`).ReplaceAllString(element, ""))
	}
	return out
}

// chromeFrame renders the frame with the doubles the package already owns: an
// authorizer that answers one way, a storybook that may or may not be there, a
// person the port answers with, and no database at all.
func chromeFrame(t *testing.T, signedIn bool) string {
	t.Helper()
	return chromeFrameWith(t, signedIn, peopleDouble{person: Person{
		DisplayName: "Ada Lovelace", Email: "ada@acme.example",
	}})
}

// peopleDouble is the whole port — one read, one answer — so each case below is a
// line of markup rather than a mock framework. The user module answers this one.
type peopleDouble struct {
	person Person
	err    error
}

func (d peopleDouble) Person(context.Context, uuid.UUID) (Person, error) {
	return d.person, d.err
}

// chromeTestFrame is the frame with these doubles, so a case can render it against
// any page.Request rather than the one this file cares about.
func chromeTestFrame(t *testing.T, people People) page.Frame {
	t.Helper()
	shellAddresses := addresses{
		workspace: route{"/", "/app"},
		dashboard: route{"/", "/app"},
		assets:    route{"/assets", "/app/app/admin/assets"},
		health:    route{"/health", "/app/app/admin/health"},
		gallery:   route{"/_gallery", "/app/app/admin/_gallery"},
	}
	storybook := func(context.Context) (export.Storybook, error) {
		return export.Storybook{}, problem.New(http.StatusForbidden, "no storybook")
	}
	return frame(shellAddresses, page.NewNavigation(nil, nil, nil), allow(true), storybook, people)
}

func chromeFrameWith(t *testing.T, signedIn bool, people People) string {
	t.Helper()
	shellAddresses := addresses{
		workspace: route{"/", "/app"},
		dashboard: route{"/", "/app"},
		assets:    route{"/assets", "/app/app/admin/assets"},
		health:    route{"/health", "/app/app/admin/health"},
		gallery:   route{"/_gallery", "/app/app/admin/_gallery"},
	}
	storybook := func(context.Context) (export.Storybook, error) {
		return export.Storybook{}, problem.New(http.StatusForbidden, "no storybook")
	}
	r := page.Request{Tenant: tenancy.Tenant{Name: "End to end"}, Path: "/app", Host: "app.acme.test"}
	if signedIn {
		r.SignedIn = true
		r.Principal = tenancy.Principal{UserID: uuid.MustParse("8258e4cc-0000-4000-8000-000000000000"), Roles: []string{"admin"}}
	}
	return render(t, frame(shellAddresses, page.NewNavigation(nil, nil, nil),
		allow(true), storybook, people)(context.Background(), r, nil))
}

// TestTheFramesChromeIsOneBodyStep. Before the constant, the same render drew
// data-size="base" for the tenant, "sm" for the caller and "xs" for the build
// stamp — three steps, which is the refusal the floor measures.
func TestTheFramesChromeIsOneBodyStep(t *testing.T) {
	t.Parallel()
	signedIn := chromeParagraphs(t, chromeFrame(t, true))
	steps := map[string]int{}
	for _, line := range signedIn {
		size := strings.SplitN(line, "\x00", 2)[0]
		steps[size]++
	}
	if len(steps) != 1 || steps[chromeTextSize] == 0 {
		t.Errorf("the signed-in chrome draws %d body steps %v, want one (%q)", len(steps), steps, chromeTextSize)
	}
	// Each of the three lines by name, so a red says which one fell back to a
	// step of its own rather than "some paragraph was wrong".
	for _, want := range []struct{ size, text string }{
		{chromeTextSize, "End to end"},   // the workspace, named in the header below the sidebar's breakpoint
		{chromeTextSize, "Ada Lovelace"}, // the caller, by the name they chose
		{chromeTextSize, brand},          // the build stamp
	} {
		found := false
		for _, line := range signedIn {
			if strings.Contains(line, want.text) && strings.HasPrefix(line, want.size+"\x00") {
				found = true
			}
		}
		if !found {
			t.Errorf("no chrome paragraph at size %q says %q; the frame drew %q", want.size, want.text, signedIn)
		}
	}
}

// TestAnAnonymousFrameKeepsTheOneStep: the caller line is a session fact, so two
// lines remain — and the remaining ones still agree.
func TestAnAnonymousFrameKeepsTheOneStep(t *testing.T) {
	t.Parallel()
	for _, line := range chromeParagraphs(t, chromeFrame(t, false)) {
		if size := strings.SplitN(line, "\x00", 2)[0]; size != chromeTextSize {
			t.Errorf("anonymous chrome paragraph is size %q, want %q (%q)", size, chromeTextSize, line)
		}
	}
}

// TestChromeTextSizeIsWhatTheFloorAllowsTwoOf: the constant is a decision a
// reviewer can disagree with, so the reason it is `sm` is asserted rather than
// commented — it is the step the sidebar's nav links already take, and it leaves
// the page's own `base` copy free to use the second size the floor allows.
func TestChromeTextSizeIsWhatTheFloorAllowsTwoOf(t *testing.T) {
	t.Parallel()
	if chromeTextSize != "sm" {
		t.Errorf("chromeTextSize = %q; the page's own copy is base, and the floor allows two", chromeTextSize)
	}
	for _, line := range chromeParagraphs(t, chromeFrame(t, true)) {
		if size := strings.SplitN(line, "\x00", 2)[0]; size != chromeTextSize {
			t.Errorf("the chrome draws a second step %q, which leaves the page's copy one", size)
		}
	}
}
