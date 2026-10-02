package components

// frame_floor_test.go pins the five properties of the frame the design floor
// refuses without a browser: the tabbable brand link carries its own colour,
// the inverse column names the colour that reads on it, the sentence in the
// footer is bounded, a heading breaks a token no dictionary has, and so does
// the breadcrumb carrying a record's name. Each is also a rule the sheet must
// carry: ui/style/emission_test.go asserts that every enumerable class —
// BreakAnywhere with them — compiles to a declaration. What that sheet paints
// on a real element is read in the browser by e2e/frame-floor.spec.ts, because
// Compose lives above this package.

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

// TestTheBreadcrumbBreaksTheNameItCarries: the current crumb is a row's name, and a name can be one
// token nobody can hyphenate. overflow-wrap inherits, so the rule sits on the list and covers every
// crumb, its separator and the links between them.
func TestTheBreadcrumbBreaksTheNameItCarries(t *testing.T) {
	t.Parallel()
	out := draw(t, Breadcrumb(BreadcrumbProps{Items: []BreadcrumbItem{
		{Label: "Tasks", Href: "/app/task/tasks"},
		{Label: strings.Repeat("a", 60), Current: true},
	}}))
	if cls := classOf(t, out, "<ol", "<ol"); !hasClass(cls, "break-anywhere") {
		t.Errorf("the breadcrumb list carries %q, so the record's own name widens the page it is on", cls)
	}
}

// TestAHeadingBreaksAnUnbreakableToken: overflow-wrap: anywhere is the value
// that takes part in intrinsic min-content sizing, so the long token in a
// heading stops setting the width of the column it sits in. break-words is not,
// which is why it would have been a rule that reads like the fix.
func TestAHeadingBreaksAnUnbreakableToken(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		level int
		tag   string
	}{{1, "<h1 "}, {2, "<h2 "}, {3, "<h3 "}} {
		out := draw(t, Heading(HeadingProps{Level: tc.level, Text: "aaaaaaaaaaaa"}))
		if cls := classOf(t, out, tc.tag, tc.tag); !hasClass(cls, "break-anywhere") {
			t.Errorf("an h%d carries %q, so one long token in it widens the page", tc.level, cls)
		}
	}
}
