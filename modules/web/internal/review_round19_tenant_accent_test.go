package internal

// Review round 19 (T-0107). The tenant's brand colour is the one colour a person
// can choose in this application, and it is painted as a token.
//
//	// modules/web/internal/mount.go
//	if colour.MatchString(settings.PrimaryColor) {
//		v.Head = []g.Node{h.StyleEl(g.Raw(":root{--pk-color-accent-default:" + settings.PrimaryColor + "}"))}
//	}
//
// T-0107's own README closes the loop it opened: "the hand-written rules are the
// exception and are named for what they are — base() and componentState() in
// ui.go, and a module's own sheet, reach for tokens — and bodyContrast below
// measures every colour they paint, so no hand-written rule can paint a boundary
// no gate measures." This is a hand-written rule, in this module, painting a
// token by name — and the colour behind it is a varchar the tenant owns.
//
// Nothing measures it. `SiteSettings.Validate` (modules/site/contracts/site.go)
// refuses anything that is not six hex digits and refuses nothing for what those
// six digits would look like; `--pk-color-accent-default` is the token behind
// `--pk-role-fg-link`, `--pk-role-fg-brand` and the fill of every primary and
// tone button, so `accent-default`/`surface-canvas` at 4.5:1, `fg-on-brand` on
// that fill at 4.5:1 and `border-primary` on the hovered fill at 3:1 are all
// painted from a value the gate never sees. Cascade layers (T-0108, merged)
// make it worse rather than better: Compose writes the tokens into a @layer and
// an *unlayered* declaration outranks every layer, which is what the comment in
// mount.go says it is aiming at ("Unlayered is what lets a tenant palette
// outrank every layer").
//
// The asymmetry is the finding. `design.Client.Resolve` refuses the same seven
// characters — the run below prints both measurements for the same colour — so
// the file an operator files is measured and the row a tenant edits is not, and
// the tenant's row wins the cascade over the operator's file. The kernel's own
// test in this package (render_test.go) pins `#c0ffee` reaching the head, which
// is 1.19:1 as link text on the light canvas: the defect is pinned, not new.
//
// The case asks one thing of this module, and it is the same thing rounds 7 to 18
// asked of a paint: a colour the shipped application puts on a page is a colour
// the one gate would issue. It is written as a relationship between the door and
// the paint, so it has a way to pass that does not prescribe the fix — refuse the
// unreadable colour where the entity is validated (modules/site's `Validate`,
// which runs on every write whichever door it came through), or repair it here
// before pinning it the way `generatedTheme` repairs an edge, and the loop below
// narrows to colours that read. The final paragraph stops the loop passing by
// refusing everything: a colour the gate itself issues — this installation's own
// accent — still has to reach the page.
//
// The column's default, #2563eb, is measured in the round's report rather than
// asserted here: it reads 4.4979:1 on the light canvas and 3.55:1 on the dark
// one, so it fails this gate too, and a case that can only go green when somebody
// changes a shipped default and its migration is a report, not a test.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui/page"
)

// accentPin reads the one declaration this module writes outside ui.Compose out
// of the head it renders. It is the reachability probe: the fixed behaviour
// prints it too, for every colour that reads, so a green run cannot be bought by
// dropping the declaration.
var accentPin = regexp.MustCompile(`--pk-color-accent-default:(#[0-9a-fA-F]{6})`)

func TestTheSitePaintsOnlyAnAccentTheContrastGateWouldIssue(t *testing.T) {
	t.Parallel()

	// Colours a tenant can type, chosen for what they measure rather than to
	// flatter either door: the column's default, the value this package's own
	// test pins, a colour that is the light canvas's own, and a pole of each
	// theme. Each theme then adds its own installed accent — the colour this
	// repository's own pair ships with, which the gate issues, so a cure that
	// refuses it would be refusing the installation that says nothing about
	// colour and this case would say so.
	candidates := map[string][]string{
		"light": {sitecontracts.DefaultPrimaryColor, "#c0ffee", "#f7f7f2", "#ffffff", strings.ToUpper(design.Default().Light.AccentDefault)},
		"dark":  {sitecontracts.DefaultPrimaryColor, "#c0ffee", "#000000", "#ffffff", strings.ToUpper(design.Default().Dark.AccentDefault)},
	}
	ctx := context.Background()
	checked := 0
	for _, theme := range []string{"light", "dark"} {
		for _, hex := range candidates[theme] {
			settings := &sitecontracts.SiteSettings{Theme: theme, PrimaryColor: hex, Title: "Acme"}
			if err := settings.Validate(ctx); err != nil {
				// The entity's own door refuses it, so no page of this site can
				// ever be painted with it. Nothing to measure.
				continue
			}
			head := render(t, Site{}.view(settings, page.Request{}, "Welcome", nil).Head)
			found := accentPin.FindStringSubmatch(head)
			if found == nil {
				t.Fatalf("settings %q/%s were accepted by SiteSettings.Validate but the rendered head carries no %s, so this case can no longer see how the tenant's colour reaches the page: head = %s",
					hex, theme, "--pk-color-accent-default", head)
			}
			checked++
			// The pair the reader is shown: this installation's pair (apps/
			// platformkit composes design.Default()), with the accent replaced by
			// the colour the head just pinned — which is what the browser does,
			// unlayered, to the tokens layer ui.Compose emits.
			pair := pairWithAccent(t, theme, found[1])
			if err := pair.Check(); err != nil {
				t.Errorf("the site paints %s as its accent on %s pages and the token gate refuses that pair: %v", found[1], theme, err)
			}
			if err := pair.CheckRoles(design.RoleLayer(), design.GatedRolePairs()); err != nil {
				t.Errorf("the site paints %s as its accent on %s pages and the role gate refuses that pair — the same measurement design.Client.Resolve applies to a colour an operator files in design.yaml: %v",
					found[1], theme, err)
			}
		}
	}
	// A pass cannot be bought by refusing the readable: the accent this
	// installation already ships is the colour a tenant gets by choosing it, and
	// the gate issues it, so it has to survive the loop in both themes. That is
	// what makes checked > 0 a statement about paint reaching a page rather than
	// about a door that refuses everything it is offered.
	if checked < 2 {
		t.Errorf("only %d of the colours this case offered reached a page: the installed accent of each theme has to reach the head, or this case measures no paint and a tenant's colour reaches nothing either", checked)
	}
}

// pairWithAccent returns design.Default() with the accent-default token of the
// named theme replaced, which is the substitution an unlayered :root declaration
// makes in the browser.
func pairWithAccent(t *testing.T, theme, hex string) design.Pair {
	t.Helper()
	pair := design.Default()
	replaced := pair.Light
	if theme == "dark" {
		replaced = pair.Dark
	}
	replaced.AccentDefault = hex
	if theme == "dark" {
		pair.Dark = replaced
	} else {
		pair.Light = replaced
	}
	return pair
}
