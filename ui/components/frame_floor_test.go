package components

// frame_floor_test.go pins the four properties of the frame the design floor
// refuses without a browser: the tabbable brand link carries its own colour,
// the inverse column names the colour that reads on it, the sentence in the
// footer is bounded, and the content region — not the heading, not the
// breadcrumb — carries the one rule that breaks a token no dictionary has. Each
// is also a rule the sheet must carry: ui/style/emission_test.go asserts that
// every enumerable class — BreakAnywhere with them — compiles to a declaration.
// What that sheet paints on a real element is read in the browser by
// e2e/frame-floor.spec.ts, because Compose lives above this package.

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"
)

// draw renders one node to a string.
func draw(t *testing.T, node g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := node.Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// classOf returns the class attribute of the first `tag` opened after `anchor`.
// It reads the attribute of one named element rather than searching the whole
// document, so a case can say which element it means — the brand link, not "a
// string appears somewhere in the sidebar".
func classOf(t *testing.T, markup, anchor, tag string) string {
	t.Helper()
	at := strings.Index(markup, anchor)
	if at < 0 {
		t.Fatalf("markup lacks the anchor %q:\n%s", anchor, markup)
	}
	open := markup[at:]
	if !strings.HasPrefix(open, "<") {
		open = open[strings.Index(open, "<"):]
	}
	start := strings.Index(open, tag)
	if start < 0 {
		t.Fatalf("nothing after %q opens a %s:\n%s", anchor, tag, markup)
	}
	open = open[start:]
	end := strings.Index(open, ">")
	if end < 0 {
		t.Fatalf("unterminated %s:\n%s", tag, markup)
	}
	const class = `class="`
	from := strings.Index(open[:end], class)
	if from < 0 {
		t.Fatalf("%s carries no class attribute: %s", tag, open[:end])
	}
	rest := open[from+len(class) : end]
	return rest[:strings.Index(rest, `"`)]
}

// hasClass reports whether the class attribute names the utility as a whole
// word, which is the only way a class list carries it.
func hasClass(class, want string) bool {
	return strings.Contains(" "+class+" ", " "+want+" ")
}

// TestTheBrandLinkCarriesItsOwnColour: the link is tabbable, so the floor reads
// it, and `a { color: inherit }` hands it the column's text colour — a
// foreground token on the admin's inverse column, 1.02:1 measured. The flavour
// decides which token, because the two columns differ in lightness and no token
// is legible on both.
func TestTheBrandLinkCarriesItsOwnColour(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		flavor string
		want   string
	}{
		{"admin", "text-fg-on-inverse"}, // reads on the inverse column
		{"content", "text-fg-primary"},  // reads on SurfacePrimary, where FgOnInverse is 1.10:1
	} {
		for _, collapsed := range []bool{false, true} {
			out := draw(t, Sidebar(SidebarProps{
				BrandLabel: "Acme", BrandHref: "/app", Flavor: tc.flavor, Collapsed: collapsed,
			}))
			for _, element := range []string{"<a ", "<span "} {
				got := classOf(t, out, `data-sidebar-brand`, element)
				if !hasClass(got, tc.want) {
					t.Errorf("%s brand %s (collapsed=%v) carries %q, which lacks %q",
						tc.flavor, element, collapsed, got, tc.want)
				}
				// One colour utility: two single-class rules have equal specificity, so the
				// sheet's alphabetical order would pick the winner.
				if n := strings.Count(got, "text-fg-"); n != 1 {
					t.Errorf("%s brand %s carries %d fg colour utilities (%q); one decides, the rest is a lottery",
						tc.flavor, element, n, got)
				}
			}
		}
	}
}

// TestTheFooterSentenceIsBounded: the footer spans the column, so an unbounded
// sentence in it measured 189 characters at 1440px — two and a half times the
// measure the floor allows. The bound sits on a wrapper, because a max-width on
// the footer itself would cut its own top border, which is the frame's rule.
func TestTheFooterSentenceIsBounded(t *testing.T) {
	t.Parallel()
	out := draw(t, Shell(ShellProps{}, ShellSlots{
		Footer: []g.Node{Text(TextProps{Content: "PlatformKit abc1234", Size: "sm", Color: "muted"})},
	}))
	if got := classOf(t, out, "<footer", "<div "); !hasClass(got, "max-w-sm") {
		t.Errorf("the footer's child carries %q, which bounds nothing", got)
	}
	if strings.Contains(classOf(t, out, "<footer", "<footer"), "max-w-") {
		t.Error("the footer bounds itself, which cuts the frame's own top border")
	}
}

// TestTheInverseColumnNamesTheColourThatReadsOnIt: the floor's probe reads every text element, not
// only links — `li` is in its contrast set — so an element inside the inverse column that chooses no
// colour of its own inherits the page's foreground token and is measured against the dark column.
// Measured at 1440px on a generated page: the <li> around a nav link, 1.02:1. The column therefore
// names the colour that reads on what it paints; the flavour that paints a light column needs
// nothing, because the inherited foreground already reads on it.
func TestTheInverseColumnNamesTheColourThatReadsOnIt(t *testing.T) {
	t.Parallel()
	admin := draw(t, Sidebar(SidebarProps{BrandLabel: "Acme", BrandHref: "/app"}))
	found := false
	for _, seg := range strings.Split(admin, `class="`)[1:] {
		end := strings.Index(seg, `"`)
		if end < 0 {
			continue
		}
		cls := seg[:end]
		if !hasClass(cls, "bg-surface-inverse") {
			continue
		}
		found = true
		if !hasClass(cls, "text-fg-on-inverse") {
			t.Errorf("the element that paints the inverse column carries %q, so everything in it that "+
				"chooses no colour inherits a foreground token and reads at 1.02:1", cls)
		}
	}
	if !found {
		t.Fatal("the admin sidebar paints no inverse column, so this case measures nothing")
	}
	if content := draw(t, Sidebar(SidebarProps{BrandLabel: "Acme", BrandHref: "/app", Flavor: "content"})); strings.Contains(content, "bg-surface-inverse") {
		t.Error("the content flavour paints an inverse column, which its own link colour does not read on")
	}
}

// TestTheFrameBreaksATokenNothingCanHyphenate: a row's name can be one token nobody can hyphenate — a
// UUID pasted into a title, a commit hash, a URL — and a token with no break opportunity sets the
// min-content width of the column it sits in, so the page scrolls sideways. overflow-wrap: anywhere is
// the value that takes part in that sizing and the property inherits, so the frame's content region
// carries the one rule and every heading, breadcrumb and table cell below it breaks. It may not sit on
// those components: the design tool builds text only where the element itself computes ordinary line
// breaking (`overflow-wrap: normal`), so a break rule on Heading or Breadcrumb leaves every card,
// toolbar and document that contains one out of a client's design document. What the browser does with
// the inherited value is read in e2e/frame-floor.spec.ts.
func TestTheFrameBreaksATokenNothingCanHyphenate(t *testing.T) {
	t.Parallel()
	out := draw(t, Shell(ShellProps{}, ShellSlots{
		Main: []g.Node{Heading(HeadingProps{Level: 1, Text: "Album"})},
	}))
	if cls := classOf(t, out, "<main", "<main"); !hasClass(cls, "break-anywhere") {
		t.Errorf("the content region carries %q, so one unbreakable name widens the page it is on", cls)
	}
	// One rule, not three: the components the region holds carry none of their own, which is what keeps
	// them projectable, and a second rule on a descendant would be the same defect one level down.
	crumbs := draw(t, Breadcrumb(BreadcrumbProps{Items: []BreadcrumbItem{
		{Label: "Tasks", Href: "/app/task/tasks"},
		{Label: strings.Repeat("a", 60), Current: true},
	}}))
	for _, component := range []struct{ anchor, tag, markup string }{
		{"<main", "<h1 ", out},
		{"<ol", "<ol ", crumbs},
	} {
		if cls := classOf(t, component.markup, component.anchor, component.tag); hasClass(cls, "break-anywhere") {
			t.Errorf("%s carries %q, so the design tool refuses it as text that breaks mid-word: "+
				"the frame's content region already sets the rule and it inherits", component.tag, cls)
		}
	}
}
