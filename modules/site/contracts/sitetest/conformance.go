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
// says so in writing. What the port does have is eleven refusals on one
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
	// Published is the events the implementation has published, in order. Half
	// of what Save promises is silence: a screen that submits its form twice
	// must not invalidate a cache twice.
	Published func() []string
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
					refuses("a colour in another notation is refused", configured,
						&contracts.SiteSettings{Title: "Acme", PrimaryColor: "rgb(1,2,3)"}),
					refuses("a theme outside the three is refused", configured,
						&contracts.SiteSettings{Title: "Acme", Theme: "neon"}),

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
	porttest.Stale: "the settings carry no revision; a save that changes nothing is silent instead, " +
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

// acme is a site somebody has configured.
func acme() *contracts.SiteSettings {
	return &contracts.SiteSettings{
		Title: "Acme", Tagline: "We make things", HomeSlug: "welcome",
		Theme: contracts.ThemeDark, PrimaryColor: "#ff8800",
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
				case got.PrimaryColor != contracts.DefaultPrimaryColor:
					t.Errorf("the colour is %q, want %q", got.PrimaryColor, contracts.DefaultPrimaryColor)
				case got.Title != "" || len(got.Nav) != 0:
					t.Errorf("an unconfigured site says %+v", got)
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
				case got.Theme != contracts.ThemeDark || got.PrimaryColor != "#ff8800":
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
	}
}
