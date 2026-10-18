// Package sitetest is the conformance suite for contracts.Service, and a fake
// that passes it.
//
// It exists because an interface is justified by a passing fake and not by a
// second production implementation (AGENTS.md rule 8). RunService is the
// specification written as executable cases; the real service and the fake both
// run it.
//
// The suite is a kit/porttest description, and this port is the shape the
// description was least sure of: a per-tenant singleton. There is no row an id
// names — every tenant has a site whether or not anybody has saved anything
// about it — so the floor's "an unknown row is not found" is not owed here and
// says so in writing. What the port does have is eighteen refusals on one
// operation, which is the shape the description is for: each is four lines
// here, and each gets the whole floor of a refusal — the error, its class, an
// unchanged site and silence — where the loops this replaces asserted the error
// alone.
package sitetest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/modules/site/contracts"
)

// Fixture is one case's world: a Service, the transaction its operations take,
// and what has been published so far.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Palette is what this case's installation draws itself with, the same value
	// the harness built the Service from. A case names a colour through it rather
	// than writing one out, so the suite says which accent it is pretending and the
	// assertions below stay true when this file's answer changes.
	Palette contracts.Palette
	// Locked is the same service as contracts.LockedReader — the read a caller
	// about to write is handed — so a case can ask whether the two reads answer the
	// same thing. Every implementation here is one; a third party's that is not
	// gets a failure naming the interface rather than a skipped case.
	Locked contracts.LockedReader
	// Published is the events the implementation has published, in order. Half
	// of what Save promises is silence: a screen that submits its form twice
	// must not invalidate a cache twice.
	Published func() []string
}

// silent is the silence half of what Save promises, for the hand-written cases
// the description's own silence check does not cover.
func (f Fixture) silent(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	if after := f.Published(); len(after) != before {
		t.Errorf("%s published %v; saving what is already stored changes nothing, so it says nothing", what, after[before:])
	}
}

func (f Fixture) one(t *testing.T, what string, step func()) {
	t.Helper()
	before := len(f.Published())
	step()
	got := f.Published()[before:]
	if len(got) != 1 || got[0] != contracts.EventSettingsUpdated {
		t.Errorf("%s published %v, want [%s]", what, got, contracts.EventSettingsUpdated)
	}
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	porttest.Run(t, Suite(h))
}

// Suite is the port, described. A consumer that wants the case names without
// running them reads porttest.Names(sitetest.Suite(h)).
func Suite(h Harness) porttest.Suite[Fixture] {
	return porttest.Suite[Fixture]{
		Port:     "contracts.Service",
		World:    h,
		Events:   func(f Fixture) []string { return f.Published() },
		Classify: classify,
		Ops: []porttest.Op[Fixture]{
			{
				// A read of a singleton: nothing to make ready, nothing to get
				// wrong. It is asked the one thing a read still owes — that it
				// publishes nothing — which is what reading an unconfigured
				// site used to assert by hand.
				Name:  "Settings",
				Ready: nothingToReady,
				Call: func(f Fixture, _ uuid.UUID) (string, error) {
					got, err := f.Service.Settings(f.Ctx, f.Tx)
					return answer(got), err
				},
			},
			{
				Name: "Save", Mutates: true,
				Publishes: []string{contracts.EventSettingsUpdated},
				Ready:     nothingToReady,
				Call:      func(f Fixture, _ uuid.UUID) (string, error) { return saveAnswer(f, acme()) },
				Snapshot:  snapshot,
				Names: map[porttest.Kind]string{
					porttest.Retry: "saving what is already stored says nothing",
				},
				Refusals: []porttest.Refusal[Fixture]{
					// The four below go through the update path as well as the
					// create one: the two used to answer with different errors,
					// which is a 500 where a 422 belongs. Provoke is what puts
					// a stored site under them.
					refuses("a colour that is not #rrggbb is refused", configured,
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "red"}),
					refuses("a three-digit colour is refused", configured,
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#fff"}),
					// " #" is neither a colour nor an absence of one: space around nothing
					// is still nothing, and the only thing that makes it a colour is the grammar.
					refuses("a colour in another notation is refused", configured,
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "rgb(1,2,3)"}),
					refuses("a colour of spaces and nothing is refused", configured,
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: " #"}),
					refuses("a theme outside the three is refused", configured,
						&contracts.SiteSettings{Title: "Acme", Theme: "neon"}),

					// The contrast band, submitted over a stored site. Every colour below reads
					// under contracts.MinAccentRatio against one of the kit's two canvases — the
					// ratios are contracts.AccentRatios' own arithmetic, which the site module's
					// TestTheAccentRatiosAreTheOnesTheRuleCompared prints. Because these are
					// refusals of the port and not of one implementation, they run through Save
					// on both: a rule branched on Theme, run only on the create path, or run only
					// inside internal/ reddens here rather than passing on one side.
					refuses("an accent too dark for the dark canvas is refused", configured, // 2.08 light
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#ff8800"}),
					refuses("an accent as bright as the light canvas is refused", configured, // 1.03 light
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#c0ffee"}),
					refuses("an accent of paper on paper is refused", configured, // 1.15 light
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#ffffff"}),
					refuses("an accent of the dark canvas on itself is refused", configured, // 1.00 dark
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#0e1614"}),
					refuses("an accent that only works in the light is refused", configured, // 2.74 dark
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#1d4ed8"}),
					refuses("an accent a hair under the band's edge is refused", configured, // 2.97 light
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "#8b8b8b"}),

					// The rest go through the create path, on a site nobody has
					// configured, which is where a form first reaches them.
					refuses("a link to another site is refused", nil, navigating("https://evil.example.com")),
					refuses("a link that is not rooted is refused", nil, navigating("about-us")),
					refuses("a link to nowhere is refused", nil, navigating("")),
					refuses("a link with nothing to say is refused", nil, &contracts.SiteSettings{
						Title: "Acme", Nav: contracts.Nav{{Label: "  ", Path: "/about-us"}}}),
					refuses("a navigation is bounded", nil, &contracts.SiteSettings{
						Title: "Acme", Nav: tooManyLinks()}),
					refuses("a title is bounded", nil, &contracts.SiteSettings{
						Title: strings.Repeat("a", contracts.MaxTitle+1)}),
					refuses("a home page is named by a slug", nil, &contracts.SiteSettings{
						Title: "Acme", HomeSlug: "Welcome Home"}),
				},
				Skip: notOwedHere,
			},
		},
		Own: cases(),
	}
}

// nothingToReady is Ready for both operations: a tenant that has configured
// nothing is the state each succeeds from, and it is the state every world
// starts in.
func nothingToReady(_ *testing.T, _ Fixture) uuid.UUID { return uuid.Nil }

// notOwedHere is the four floor refusals this port does not owe, with the
// reason each is not asked.
var notOwedHere = map[porttest.Kind]string{
	porttest.Unknown: "a tenant's site is a singleton and Save takes no id: every tenant has one, " +
		"whether or not anybody has saved anything about it, so there is no row to name wrongly " +
		"and Settings never reports one missing",
	porttest.Denied: "the settings take no grant of their own: the screen is reached through " +
		"kit/rest's routes and modules/site's own mount test, and that is where the grant is checked",
	porttest.Stale: "Save takes no expected revision to check: Revision counts this row's own writes " +
		"and a caller may read it, but a save that changes nothing is silent instead of refused, " +
		"which is the retry above",
	porttest.Elsewhere: "the world is one tenant's transaction, so there is no second tenant " +
		"here to make the refused call from. The fake keeps one settings row per tenant, the way " +
		"row-level security gives the real service one, which this package's own " +
		"TestTheFakeKeepsEachTenantOnItsOwnRow pins, and the real service runs under row-level " +
		"security, which kit/db's TestTenantIsolationIsEnforcedByPostgres and kit/crud's " +
		"TestAnotherTenantReachesNothing prove against the schema",
}

// refuses is one invalid save: the settings a caller submitted, and the state
// they were submitted from. Every refusal this port has is the same shape, so
// it is written once.
func refuses(name string, from func(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID, in *contracts.SiteSettings) porttest.Refusal[Fixture] {
	return porttest.Refusal[Fixture]{
		Name: name, Class: porttest.Correctable, Provoke: from,
		Call: func(f Fixture, _ uuid.UUID) error { return saveErr(f, clone(in)) },
		Is:   func(_ Fixture, err error) bool { return errors.Is(err, crud.ErrInvalid) },
	}
}

// configured is a Provoke that stores a site first, so the refusal under it
// takes the update path.
func configured(t *testing.T, f Fixture, row uuid.UUID) uuid.UUID {
	t.Helper()
	save(t, f, acme())
	return row
}

// clone is a copy of the settings a refusal submits, because Save stamps the
// value it is handed and a case must not hand the next one a stamped copy.
func clone(in *contracts.SiteSettings) *contracts.SiteSettings {
	out := *in
	out.Nav = append(contracts.Nav(nil), in.Nav...)
	return &out
}

// classify is this port's reading of its own refusals. Every one of them is an
// input the caller can retype: a colour, a theme, a link, a length.
func classify(err error) porttest.Class {
	switch {
	case errors.Is(err, crud.ErrInvalid):
		return porttest.Correctable
	case errors.Is(err, crud.ErrConflict), errors.Is(err, crud.ErrNotFound):
		return porttest.Immutable
	default:
		return porttest.Unclassified
	}
}

// snapshot is the whole site as a reader sees it, rendered so that two readings
// compare with ==. The id is in it because a tenant has one site: a second save
// that made a second row would change it.
func snapshot(t *testing.T, f Fixture, _ uuid.UUID) string {
	t.Helper()
	got, err := f.Service.Settings(f.Ctx, f.Tx)
	if err != nil {
		return "no settings: " + err.Error()
	}
	return fmt.Sprintf("id=%s title=%q tagline=%q home=%q theme=%s colour=%s nav=%v",
		got.ID, got.Title, got.Tagline, got.HomeSlug, got.Theme, got.PrimaryColor, got.Nav)
}

// InstallationPalette is the accent pair the installation this suite pretends to
// be is drawn with. Both harnesses build their service from it and hand it back as
// Fixture.Palette, so the fake's cases are the SQL service's cases.
//
// Both values are colours the accent rule *refuses* — #0f5d4e reads 6.78:1 against
// the light canvas and 2.36:1 against the dark one, #4bbf9c the other way round —
// and that is the point: the installation's own accent is not a colour a tenant
// could choose, so every case that sends it back is a form echoing a read and not
// a hand typing a colour. A suite whose palette were a storable colour could not
// tell those two apart.
var InstallationPalette = contracts.PaletteFunc(func(mode string) string {
	if mode == contracts.ThemeDark {
		return "#4bbf9c"
	}
	return "#0f5d4e"
})

// acme is a site somebody has configured. Its accent is inside the band the
// contrast rule keeps — #b45309 reads 4.37:1 on the light canvas and 3.66:1 on the
// dark one — because this fixture is the input every accepted case saves, and the
// colour the kit used to ship, #ff8800, is now a refusal input and stays in this
// file as one.
func acme() *contracts.SiteSettings {
	return &contracts.SiteSettings{
		Title: "Acme", Tagline: "We make things", HomeSlug: "welcome",
		Theme: contracts.ThemeDark, PrimaryColor: "#b45309",
		Nav: contracts.Nav{{Label: "About", Path: "/about-us"}, {Label: "Blog", Path: "/blog"}},
	}
}

// navigating is a site whose one link points at path.
func navigating(path string) *contracts.SiteSettings {
	return &contracts.SiteSettings{Title: "Acme", Nav: contracts.Nav{{Label: "About", Path: path}}}
}

// tooManyLinks is one link more than a navigation holds.
func tooManyLinks() contracts.Nav {
	long := contracts.Nav{}
	for range contracts.MaxNav + 1 {
		long = append(long, contracts.NavItem{Label: "Link", Path: "/link"})
	}
	return long
}

// save is Save with the error checked, which every case but the refusals does.
func save(t *testing.T, f Fixture, in *contracts.SiteSettings) *contracts.SiteSettings {
	t.Helper()
	out, err := f.Service.Save(f.Ctx, f.Tx, in)
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	return out
}

func saveErr(f Fixture, in *contracts.SiteSettings) error {
	_, err := f.Service.Save(f.Ctx, f.Tx, in)
	return err
}

// saveAnswer is Save and what it answered, rendered. The retry reads it either
// side of the second save: "saving what is already stored says nothing" is a
// sentence about the answer too, and a second save that answered a site with a
// new id would have made a second row for a tenant that has one.
func saveAnswer(f Fixture, in *contracts.SiteSettings) (string, error) {
	out, err := f.Service.Save(f.Ctx, f.Tx, in)
	return answer(out), err
}

// answer renders what a call handed back, so that two answers compare with ==.
// It is the site as the caller was told it stands, which is not the same
// assertion as the site as stored: a service that stores the right row and
// answers another returns a stale row to its caller.
func answer(got *contracts.SiteSettings) string {
	if got == nil {
		return "nothing"
	}
	return fmt.Sprintf("id=%s title=%q tagline=%q home=%q theme=%s colour=%s nav=%v",
		got.ID, got.Title, got.Tagline, got.HomeSlug, got.Theme, got.PrimaryColor, got.Nav)
}

// cases is what the description cannot express, each with the reason it is
// written by hand.
func cases() []porttest.Case[Fixture] {
	return []porttest.Case[Fixture]{
		{
			Name:    "a tenant that has configured nothing has the defaults",
			Because: "what a read answers before anybody has written is the port's domain: system, not dark, and a colour somebody chose for everyone",
			Run: func(t *testing.T, f Fixture) {
				got, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				switch {
				case got.Theme != contracts.ThemeSystem:
					t.Errorf("theme is %q, want %q: a tenant that has said nothing has not chosen dark",
						got.Theme, contracts.ThemeSystem)
				case got.PrimaryColor != f.Palette.Accent(contracts.ThemeLight):
					// The stored colour is DefaultPrimaryColor — nobody chose one — and what
					// the read answers is the installation's own. A tenant that has never
					// opened the form is never handed the empty colour to do with as it likes.
					t.Errorf("the colour is %q, want the installation's %q",
						got.PrimaryColor, f.Palette.Accent(contracts.ThemeLight))
				case got.Title != "" || len(got.Nav) != 0:
					t.Errorf("an unconfigured site says %+v", got)
				case got.Revision != 0:
					t.Errorf("an unconfigured site is at revision %d; the number counts writes", got.Revision)
				}
			},
		},
		{
			Name:    "saving records what was configured",
			Because: "domain contents: every field reads back as it was written, and the navigation in the order it was written in",
			Run: func(t *testing.T, f Fixture) {
				if out := save(t, f, acme()); out.ID == uuid.Nil {
					t.Error("the saved settings have no id")
				}
				got, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				switch {
				case got.Title != "Acme" || got.Tagline != "We make things" || got.HomeSlug != "welcome":
					t.Errorf("the settings read back as %+v", got)
				case got.Theme != contracts.ThemeDark || got.PrimaryColor != "#b45309":
					t.Errorf("the look reads back as %q/%q", got.Theme, got.PrimaryColor)
				case len(got.Nav) != 2 || got.Nav[0].Label != "About" || got.Nav[1].Path != "/blog":
					t.Errorf("the navigation reads back as %+v, and its order is the order it was written in", got.Nav)
				}
			},
		},
		{
			Name:    "changing one thing says so",
			Because: "the generated success saves onto an unconfigured site; this is the other half of the same promise — a second save that does change something is news, and it is still one site",
			Run: func(t *testing.T, f Fixture) {
				first := save(t, f, acme())
				changed := acme()
				changed.Nav = append(changed.Nav, contracts.NavItem{Label: "Contact", Path: "/contact"})
				var out *contracts.SiteSettings
				f.one(t, "adding a link", func() { out = save(t, f, changed) })
				if out.ID != first.ID || len(out.Nav) != 3 {
					t.Errorf("the changed settings are %s with %d links", out.ID, len(out.Nav))
				}
			},
		},
		{
			Name:    "a site with no home page is saved",
			Because: "the success half of \"a home page is named by a slug\": empty is not a refusal, because a site is configurable before anything is published at any name, and a description states a refusal or a success and not both",
			Run: func(t *testing.T, f Fixture) {
				if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme"}); err != nil {
					t.Errorf("a site with no home page: %v", err)
				}
			},
		},
		{
			// The other side of the six contrast refusals above, and the reason they are
			// six separate cases: a description states a refusal or a success, not both.
			// #8a8a8a and #8b8b8b are one step apart — 3.0044:1 and 2.9651:1 against the
			// light canvas — and this pair sits here, so the band's edge is pinned to the
			// step between them rather than rounded away. It cannot tell `>=` from `>`;
			// no #rrggbb colour reads exactly the threshold on these two canvases. A
			// comparison made on rounded ratios refuses #8a8a8a, and this case is red.
			Name:    "an accent inside the band is accepted",
			Because: "the success half of the contrast refusals, and where the band's edge is pinned: a description states a refusal or a success and not both",
			Run: func(t *testing.T, f Fixture) {
				save(t, f, acme())
				for _, colour := range []string{
					"#2563eb", // 4.50 / 3.55 — a blue inside the band, and one a tenant may choose
					"#b45309", // 4.37 / 3.66
					"#0f766e", // 4.76 / 3.36
					"#7c3aed", // 4.96 / 3.22
					"#7f7f7f", // 3.48 / 4.59 — a mid grey clears both by a wider margin than either extreme
					"#8a8a8a", // 3.0044 / 5.32 — the nearest colour above the band's edge
				} {
					if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: colour}); err != nil {
						t.Errorf("saving %q: %v: a colour that reads at least %g:1 against both canvases is accepted", colour, err, contracts.MinAccentRatio)
					}
				}
			},
		},
		{
			// A colour is compared, not guessed: the trim and the lower-case happen
			// first, so the same colour typed two ways is stored one way.
			Name:    "a colour typed with room and case is the colour it means",
			Because: "the shape of a refusal is the refusal's own; what a storable colour is, and in what spelling it is stored, is this port's domain and no description states it",
			Run: func(t *testing.T, f Fixture) {
				out := save(t, f, &contracts.SiteSettings{Title: "Acme", PrimaryColor: " #2563EB "})
				if out.PrimaryColor != "#2563eb" {
					t.Errorf("the stored colour is %q, want %q", out.PrimaryColor, "#2563eb")
				}
				_, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", PrimaryColor: "#2563E b"})
				mustBe(t, err, crud.ErrInvalid)
			},
		},
		{
			// The read's answer is a storable document, and saving it back is not an
			// edit: a settings form submits what the read gave it, so a save that stored
			// the installation's accent as this tenant's own would freeze them at the
			// palette of the day, bump the revision, invalidate a cache and put a line in
			// the trail that nobody typed.
			Name:    "the read's answer, saved back, is not a change",
			Because: "the generated retry saves the same input twice; this one saves what the read answered, which is a different document from the one anybody typed",
			Run: func(t *testing.T, f Fixture) {
				shown, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				if shown.PrimaryColor == contracts.DefaultPrimaryColor {
					t.Fatalf("the read answered the empty colour; every door that shows these settings needs a colour")
				}
				if first := save(t, f, shown); first.Revision != 1 {
					t.Errorf("the first save left the row at revision %d", first.Revision)
				}
				again, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				var out *contracts.SiteSettings
				f.silent(t, "saving the read's answer back", func() { out = save(t, f, again) })
				if out.Revision != 1 {
					t.Errorf("echoing the read's answer moved the revision to %d; nothing was chosen, so nothing was written", out.Revision)
				}
				if out.PrimaryColor != f.Palette.Accent(contracts.ThemeLight) {
					t.Errorf("the save answered %q, want the installation's accent it was given", out.PrimaryColor)
				}
			},
		},
		{
			// The echo is compared against both of the installation's accents and not
			// against the row's own mode, and this is the case that would catch it being
			// narrowed: a form that changes light to dark sends the accent the read gave
			// for light. Stored as itself that colour meets the legibility rule and is
			// refused — a form that cannot change its theme because it never chose a
			// colour.
			Name:    "the theme can change while the accent stays unchosen",
			Because: "it sends the read's answer for one mode and asks the other mode's answer back, which is a rule about two reads and one write, not a refusal of an input",
			Run: func(t *testing.T, f Fixture) {
				if _, err := f.Service.Save(f.Ctx, f.Tx, &contracts.SiteSettings{Title: "Acme", Theme: contracts.ThemeLight}); err != nil {
					t.Fatalf("save a light site: %v", err)
				}
				shown, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				if shown.PrimaryColor != f.Palette.Accent(contracts.ThemeLight) {
					t.Fatalf("a light site with no accent reads %q", shown.PrimaryColor)
				}
				shown.Theme = contracts.ThemeDark
				save(t, f, shown)
				got, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				if got.PrimaryColor != f.Palette.Accent(contracts.ThemeDark) {
					t.Errorf("the dark site reads %q, want %q: the theme moved and the accent is still nobody's",
						got.PrimaryColor, f.Palette.Accent(contracts.ThemeDark))
				}
			},
		},
		{
			// A colour somebody did choose is not the installation's, whichever door it
			// is read from.
			Name:    "an accent the tenant chose survives every door",
			Because: "it asks the locked read, which the description has no channel for: Save and Settings are the two operations the floor runs",
			Run: func(t *testing.T, f Fixture) {
				save(t, f, acme())
				got, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				if got.PrimaryColor != "#b45309" {
					t.Errorf("Settings answers %q, want the #b45309 this tenant wrote", got.PrimaryColor)
				}
				locked, err := f.Locked.SettingsForUpdate(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("SettingsForUpdate: %v", err)
				}
				if locked.PrimaryColor != got.PrimaryColor || locked.Revision != got.Revision {
					t.Errorf("the locked read answers %q at revision %d and the unlocked read %q at %d",
						locked.PrimaryColor, locked.Revision, got.PrimaryColor, got.Revision)
				}
			},
		},
		{
			// Both reads are the same answer, which is what lets a change proposal be
			// diffed against the document the person was shown.
			Name:    "the locked read says what the unlocked read says",
			Because: "a second read of the same row is not one of the port's operations: contracts.LockedReader is a door on Settings, and the floor runs two operations",
			Run: func(t *testing.T, f Fixture) {
				unlocked, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				locked, err := f.Locked.SettingsForUpdate(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("SettingsForUpdate: %v", err)
				}
				if locked.PrimaryColor != unlocked.PrimaryColor {
					t.Errorf("the locked read answers %q and the plain read %q", locked.PrimaryColor, unlocked.PrimaryColor)
				}
				if locked.Revision != unlocked.Revision {
					t.Errorf("the locked read is at revision %d and the plain read at %d", locked.Revision, unlocked.Revision)
				}
			},
		},
		{
			// The installation's own accent is not a colour this tenant may hold, and the
			// door that receives it is the one that says so: a form that sends either
			// accent back is answered the installation's own colour for the mode it is on,
			// which — for the accent of the *other* mode — is not the value it sent.
			Name:    "the installation's own accent is not a tenant's colour",
			Because: "it is not a refusal at all: the write is accepted and the echoed value is cleared, which is a third answer no Refusal of this description can be",
			Run: func(t *testing.T, f Fixture) {
				save(t, f, &contracts.SiteSettings{
					Title: "Acme", Theme: contracts.ThemeLight,
					PrimaryColor: f.Palette.Accent(contracts.ThemeDark),
				})
				got, err := f.Service.Settings(f.Ctx, f.Tx)
				if err != nil {
					t.Fatalf("Settings: %v", err)
				}
				if got.PrimaryColor != f.Palette.Accent(contracts.ThemeLight) {
					t.Errorf("a light site sent the installation's dark accent and reads %q, want %q: the value was nobody's choice",
						got.PrimaryColor, f.Palette.Accent(contracts.ThemeLight))
				}
			},
		},
		{
			// The two numbers a shell is told are the two the rule compared — same
			// arithmetic, same rounding, one implementation.
			Name:    "the ratio the document quotes is the ratio the rule used",
			Because: "contracts.AccentRatios is arithmetic beside the port and not a call on it: nothing in the description could run it",
			Run: func(t *testing.T, f Fixture) {
				light, dark, ok := contracts.AccentRatios("#2563eb")
				if !ok {
					t.Fatal("AccentRatios cannot read a colour this rule has measured for years")
				}
				if light != 4.50 || dark != 3.55 {
					t.Errorf("#2563eb reads %.2f / %.2f, want 4.50 / 3.55", light, dark)
				}
				if _, err := contracts.ContrastRatio("mint", contracts.CanvasLight); err == nil || !strings.Contains(err.Error(), "mint") {
					t.Errorf("ContrastRatio(%q, …) = %v, want an error naming the value", "mint", err)
				}
			},
		},
	}
}

// mustBe is the assertion every hand-written refusal here makes: the error the
// port owes, whatever else it also carries.
func mustBe(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Errorf("error is %v, want %v", got, want)
	}
}
