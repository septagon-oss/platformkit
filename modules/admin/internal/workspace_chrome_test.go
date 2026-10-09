package internal

// workspace_chrome_test.go is the two sentences the frame says about who and
// where: the person signed in, and the workspace they are in.
//
// Both are chrome, so both render here — no database, no locale, no browser: the
// frame renders from a page.Request and a double. What is *not* here is the read
// itself: People is answered by a fake, and the mounted application's own suite is
// what proves a real user row reaches the same string.

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// theID is the account every case here signs in as. Its first eight bytes are what
// the frame printed where a person's name should be, so they are asserted against in
// every case rather than in one.
var theID = uuid.MustParse("8258e4cc-0000-4000-8000-000000000000")

// frameAs renders the signed-in frame as one answer of the port, at one host, for
// one tenant.
func frameAs(t *testing.T, people People, host, tenant string) string {
	t.Helper()
	r := page.Request{
		Tenant:    tenancy.Tenant{Name: tenant},
		Path:      "/app",
		Host:      host,
		SignedIn:  true,
		Principal: tenancy.Principal{UserID: theID, Roles: []string{"admin"}},
	}
	markup := render(t, chromeTestFrame(t, people)(context.Background(), r, nil))
	if !strings.Contains(markup, "<main") {
		t.Fatalf("the frame drew no body:\n%s", markup)
	}
	return markup
}

// callerLine is the chrome paragraph that names the person: not the workspace,
// which the header also says, and not the build stamp.
func callerLine(t *testing.T, markup string) string {
	t.Helper()
	for _, line := range chromeParagraphs(t, markup) {
		text := strings.SplitN(line, "\x00", 2)[1]
		if text != "End to end" && text != "app.acme.test" && !strings.HasPrefix(text, brand) {
			return text
		}
	}
	t.Fatalf("no chrome paragraph names the caller; the frame drew %q", chromeParagraphs(t, markup))
	return ""
}

// TestTheCallerIsNamedByNameThenAddressThenNothing. The id-fragment path is deleted,
// not made conditional, so no answer the port can give puts an id on the page.
func TestTheCallerIsNamedByNameThenAddressThenNothing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		people People
		want   string
	}{
		{"a name is the answer", peopleDouble{person: Person{DisplayName: "Ada Lovelace", Email: "ada@acme.example"}}, "Ada Lovelace"},
		{"an invited person has an address and no name", peopleDouble{person: Person{Email: "ada@acme.example"}}, "ada@acme.example"},
		{"a read that failed says the one thing still known", peopleDouble{err: errors.New("gone")}, "Signed in"},
		{"a row that is gone", peopleDouble{}, "Signed in"},
		{"no user module composed", nil, "Signed in"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markup := frameAs(t, tc.people, "app.acme.test", "End to end")
			if got := callerLine(t, markup); got != tc.want {
				t.Errorf("the header names the caller %q, want %q", got, tc.want)
			}
			if strings.Contains(markup, "8258e4cc") || strings.Contains(markup, theID.String()) {
				t.Errorf("an id fragment is on the page after all:\n%s", markup)
			}
		})
	}
}

// TestAnAnonymousFrameNamesNobodyAtAll: the caller line is a session fact, so an
// anonymous render draws neither a name nor the fallback that stands for one.
func TestAnAnonymousFrameNamesNobodyAtAll(t *testing.T) {
	markup := chromeFrameWith(t, false, peopleDouble{person: Person{DisplayName: "Ada Lovelace"}})
	for _, line := range chromeParagraphs(t, markup) {
		if text := strings.SplitN(line, "\x00", 2)[1]; text == "Signed in" || text == "Ada Lovelace" {
			t.Errorf("an anonymous page names somebody it cannot know: %q", text)
		}
	}
}

// TestTheWorkspaceIsNamedOnce. In Go markup the name legitimately appears twice,
// because exactly one of the two is display:none at any width — a fact about painted
// boxes, which the browser case in e2e owns. What is pinned here is the pair of
// rules that make the promise: the sidebar paints from the large breakpoint up, and
// the header's line is the mirror that is hidden from it.
func TestTheWorkspaceIsNamedOnce(t *testing.T) {
	for _, tc := range []struct{ name, tenant, host, want string }{
		{"a tenant with a name wears it", "End to end", "app.acme.test", "End to end"},
		{"without one, the host does", "", "app.acme.test", "app.acme.test"},
		{"a host arrives in any case, and its default port says nothing", "", "App.ACME.test:443", "app.acme.test"},
		{"a host that is not a host is refused", "", "evil.example.com/ x?q=1", brand},
		{"an empty host is refused", "", "", brand},
		{"a host longer than DNS allows is refused", "", strings.Repeat("a", 250) + ".example.com", brand},
		{"credentials are not a name", "", "ada:secret@acme.test", brand},
		{"a port that is not a number is not a host", "", "acme.test:x", brand},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The rule under the rule: a name that is not a host is refused before the
			// tenant's own name is even asked for.
			if got := workspace(page.Request{Tenant: tenancy.Tenant{Name: tc.tenant}, Host: tc.host}, brand); got != tc.want {
				t.Errorf("the workspace name for tenant %q at host %q = %q, want %q", tc.tenant, tc.host, got, tc.want)
			}
			markup := frameAs(t, peopleDouble{}, tc.host, tc.tenant)
			sidebar := markup[strings.Index(markup, "<aside"):]
			if !strings.Contains(sidebar, ">"+tc.want+"<") {
				t.Errorf("the sidebar does not name the workspace %q", tc.want)
			}
			if !strings.Contains(sidebar, "lg:flex") {
				t.Error("the sidebar is no longer the half that paints from the large breakpoint up")
			}
			// The header's line is the paragraph that both names the workspace and
			// carries the rule that hides it from the breakpoint the sidebar paints at.
			tag := openingTagOf(t, markup, tc.want)
			if !strings.Contains(tag, "lg:hidden") {
				t.Errorf("the header's line for the workspace %q is %q: with no rule hiding it from the large breakpoint up, the name is painted twice", tc.want, tag)
			}
		})
	}
}

// openingTagOf returns the opening tag of the paragraph that says exactly this, so a
// case can name one element rather than searching a whole document for a string the
// sidebar says too.
func openingTagOf(t *testing.T, markup, text string) string {
	t.Helper()
	for _, element := range regexp.MustCompile(`<p [^>]*>.*?</p>`).FindAllString(markup, -1) {
		// Text marks its own content with comments, so the words are read out of the
		// element rather than matched against its inside.
		said := regexp.MustCompile(`<!--.*?-->|<[^>]*>`).ReplaceAllString(element, "")
		if strings.TrimSpace(said) == text {
			return element[:strings.Index(element, ">")+1]
		}
	}
	t.Fatalf("no paragraph says %q; the frame drew:\n%s", text, markup)
	return ""
}

// TestTheReadIsAskedOfTheRequestsOwnContext: the port is handed the context the
// frame was rendered with, so the composition reads the caller's transaction off it
// and this package opens nothing of its own.
func TestTheReadIsAskedOfTheRequestsOwnContext(t *testing.T) {
	asked := &recorded{}
	type probeKey string
	ctx := context.WithValue(context.Background(), probeKey("admin-probe"), "present")
	markup := render(t, chromeTestFrame(t, asked)(ctx, page.Request{
		Tenant: tenancy.Tenant{Name: "End to end"}, Path: "/app", Host: "app.acme.test",
		SignedIn: true, Principal: tenancy.Principal{UserID: theID},
	}, nil))
	if asked.seen == nil {
		t.Fatal("the frame rendered a signed-in caller without asking who they are")
	}
	if asked.seen.Value(probeKey("admin-probe")) != "present" {
		t.Error("the port was not handed the context the frame was rendered with")
	}
	if asked.id != theID {
		t.Errorf("the port was asked about %s, not about the caller", asked.id)
	}
	if !strings.Contains(markup, ">Ada<") {
		t.Errorf("the answer the port gave never reached the page:\n%s", markup)
	}
}

// recorded is the port answering once and saying what it was asked with.
type recorded struct {
	seen context.Context
	id   uuid.UUID
}

func (r *recorded) Person(ctx context.Context, id uuid.UUID) (Person, error) {
	r.seen, r.id = ctx, id
	return Person{DisplayName: "Ada"}, nil
}
