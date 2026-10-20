package internal

// workspace_facts_disclosure_test.go is where the frame keeps what support reads
// and a person does not look for: the header names the person, and whose
// workspace the host serves and which build answered stay reachable behind one
// closed disclosure in the footer — never removed, never in the open.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/page"
)

// frameRegion is the frame's own <header> or <footer>: the first one after the
// sidebar, because the sidebar's brand is itself a <header> inside <aside>.
func frameRegion(t *testing.T, markup, tag string) string {
	t.Helper()
	aside := strings.Index(markup, "</aside>")
	if aside < 0 {
		t.Fatalf("the frame drew no sidebar:\n%s", markup)
	}
	rest := markup[aside:]
	begin := strings.Index(rest, "<"+tag)
	end := strings.Index(rest, "</"+tag+">")
	if begin < 0 || end < begin {
		t.Fatalf("the frame drew no <%s> after the sidebar:\n%s", tag, markup)
	}
	return rest[begin : end+len("</"+tag+">")]
}

// identifiers are what the frame knows a workspace or a build by and nobody chose
// to be called by.
var identifiers = []string{"Tenant workspace", "Platform operator", brand + " ", "8258e4cc"}

func TestTheHeaderNamesThePersonAndNoIdentifier(t *testing.T) {
	for _, operator := range []bool{false, true} {
		r := page.Request{
			Tenant:    tenancy.Tenant{Name: "End to end", Operator: operator},
			Path:      "/app",
			Host:      "app.acme.test",
			SignedIn:  true,
			Principal: tenancy.Principal{UserID: theID, Roles: []string{"admin"}},
		}
		markup := render(t, chromeTestFrame(t, peopleDouble{person: Person{DisplayName: "Ada Lovelace"}})(context.Background(), r, nil))
		header := frameRegion(t, markup, "header")
		if !strings.Contains(header, "Ada Lovelace") {
			t.Errorf("operator=%v: the header does not name the person:\n%s", operator, header)
		}
		for _, id := range identifiers {
			if strings.Contains(header, id) {
				t.Errorf("operator=%v: the header says %q outside the disclosure:\n%s", operator, id, header)
			}
		}
	}
}

func TestTheWorkspaceFactsAreBehindOneDisclosure(t *testing.T) {
	for _, signedIn := range []bool{true, false} {
		footer := frameRegion(t, chromeFrameWith(t, signedIn, peopleDouble{person: Person{DisplayName: "Ada Lovelace"}}), "footer")
		if n := strings.Count(footer, "<details"); n != 1 {
			t.Fatalf("signed in=%v: the footer holds %d disclosures, want 1:\n%s", signedIn, n, footer)
		}
		begin, end := strings.Index(footer, "<details"), strings.Index(footer, "</details>")
		details := footer[begin : end+len("</details>")]
		opening := details[:strings.Index(details, ">")+1]
		if regexp.MustCompile(`\sopen[\s>=]`).MatchString(opening) {
			t.Errorf("signed in=%v: the disclosure is open by default: %s", signedIn, opening)
		}
		summary := regexp.MustCompile(`<summary[^>]*>(.*?)</summary>`).FindStringSubmatch(details)
		if summary == nil || summary[1] != "About this workspace" {
			t.Errorf("signed in=%v: the disclosure is labelled %q, want %q", signedIn, summary, "About this workspace")
		}
		inside := chromeParagraphs(t, "<main></main>"+details)
		var scope, stamp bool
		for _, line := range inside {
			size, text, _ := strings.Cut(line, "\x00")
			if size != chromeTextSize {
				t.Errorf("signed in=%v: a disclosed fact is size %q, want the chrome's %q", signedIn, size, chromeTextSize)
			}
			scope = scope || text == "Tenant workspace"
			stamp = stamp || strings.HasPrefix(text, brand+" ")
		}
		if !scope || !stamp {
			t.Errorf("signed in=%v: the disclosure holds %q; want the scope and the build stamp", signedIn, inside)
		}
		if outside := footer[:begin] + footer[end:]; strings.Contains(outside, "<p") {
			t.Errorf("signed in=%v: the footer says something outside the disclosure:\n%s", signedIn, outside)
		}
	}
}
