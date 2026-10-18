// Package contracts is everything another module, an app or a test may know
// about a tenant's public site: the settings, the event, the permission and the
// Service interface. The implementation is in ../internal.
//
// This module is the data a public site is made of and none of the rendering.
// A theme reads a title, a navigation and a colour and decides what to do with
// them; E4's admin shell and whatever public theme follows it are the ones that
// turn this into HTML. Keeping the two apart is what lets a deployment replace
// the theme without touching what a tenant configured, and it is why there is
// no template anywhere in this module.
package contracts

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// The three themes. system is the absence of a choice, which is why an empty
// string means it: a tenant that has said nothing has not chosen dark.
const (
	ThemeLight  = "light"
	ThemeDark   = "dark"
	ThemeSystem = "system"
)

var themes = []string{ThemeLight, ThemeDark, ThemeSystem}

// DefaultPrimaryColor is the stored value that means this site named no accent.
// It is not a colour but the absence of one, which is why it is the empty string:
// the colour such a site is rendered in belongs to the installation and not to
// this module, and Service answers it on the way out (see Palette and
// SiteSettings.Resolve) so that no read, screen or shell is ever handed the empty
// colour. Storing the absence rather than filling in a colour on write is the
// decision: a row that recorded the palette's accent would stop following the
// palette the day the palette moved, and would be a value nobody typed.
const DefaultPrimaryColor = ""

// The bounds. A navigation with more than a dozen links is not a navigation,
// and a label longer than a few words is a sentence in a menu.
const (
	MaxNav      = 12
	MaxLabel    = 40
	MaxPath     = 200
	MaxTitle    = 120
	MaxTagline  = 200
	MaxHomeSlug = 200
)

var (
	// hexColor is #rrggbb and nothing else: three digits, a name, a gradient
	// and rgb() are all things a theme would have to parse, and a colour a
	// theme cannot parse is a site that renders in the wrong one.
	hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	// slug is the grammar modules/content produces. This module validates it
	// and does not import the module that serves it: a home page slug is a
	// string until something looks it up, and a site is configurable before
	// anything is published at that name.
	slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
)

// NavItem is one link in a public site's navigation.
type NavItem struct {
	Label string `json:"label" doc:"What the link says" example:"About"`
	// Path is a path within the site and not a URL: a navigation that could
	// carry an absolute one is a navigation somebody can use to send a tenant's
	// visitors somewhere else.
	Path string `json:"path" doc:"Path within this site" example:"/about-us"`
}

// Nav is the navigation, one jsonb column. It is a named type so the codec is
// written once, the way modules/user spells Roles and modules/billing spells
// Features; this one is JSON rather than a Postgres array because an item has
// two fields.
type Nav []NavItem

func (n Nav) Value() (driver.Value, error) { return json.Marshal(n) }

func (n *Nav) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*n = nil
		return nil
	case []byte:
		return json.Unmarshal(v, n)
	case string:
		return json.Unmarshal([]byte(v), n)
	}
	return fmt.Errorf("site: a navigation is jsonb and this is %T", src)
}

// SiteSettings is one tenant's public site, and there is one row or none:
// migrations/000018 says so with a unique index on the tenant. A tenant that
// has never saved any still has settings — the zero value below, which is what
// Service.Settings answers with.
type SiteSettings struct {
	crud.Base

	// Title is the site's name, and Tagline the line under it.
	Title   string `json:"title,omitempty" ui:"label:Site name" gorm:"type:varchar(120);not null;default:''" maxLength:"120" doc:"The site's name" example:"Acme"`
	Tagline string `json:"tagline,omitempty" gorm:"type:varchar(200);not null;default:''" maxLength:"200" doc:"The line under the name" example:"We make things"`

	// HomeSlug is the content served at the site's root. It is a slug and not
	// an id because a page can be rewritten and replaced and still be the home
	// page; it is empty until somebody chooses one.
	HomeSlug string `json:"homeSlug,omitempty" gorm:"type:varchar(200);not null;default:''" maxLength:"200" ui:"label:Home page;help:Which page is served at the site root" doc:"Slug of the content served at /" example:"welcome"`

	// Theme and PrimaryColor are the whole of what a tenant may say about how
	// their site looks. A theme that needed more would be a theme with a
	// stylesheet editor in it.
	Theme string `json:"theme" gorm:"type:varchar(10);not null;default:'system'" enum:"light,dark,system" ui:"widget:select;label:Colour scheme" doc:"Colour scheme, or system to follow the visitor's" default:"system" required:"false"`
	// No `default:` and no `example:` is published: nothing fills a colour in on
	// write any more, and the value the column holds when nobody chose one is the
	// empty colour, which the published grammar below does not admit. The write
	// grammar is the tenant's colour; the read always answers #rrggbb.
	PrimaryColor string `json:"primaryColor" gorm:"type:char(7);not null;default:''" pattern:"^#[0-9a-fA-F]{6}$" ui:"label:Brand colour" doc:"Brand colour, #rrggbb. A site that names none is rendered in the installation's own accent; a read always answers #rrggbb" required:"false"`

	// LogoFileID is a file id with no foreign key behind it, which is what
	// "cross-module dependencies are Go interfaces" costs at the database: this
	// module never names modules/file, and a logo that has been deleted is a
	// site that renders without one.
	//
	// It is labelled "Logo" and declares no `reference:`. A reference names a
	// *resource address*, and `file` registers no resource of its own: its routes
	// are its own operations on `/api/v1/file/files`, so `file/file` is not a
	// composition's promise and `rest.CheckReferences` would refuse the boot of
	// every installation that mounts this module. The label is the reading; the
	// address has to exist before it can be pointed at.
	LogoFileID *uuid.UUID `json:"logoFileId,omitempty" gorm:"type:uuid" format:"uuid" ui:"label:Logo" doc:"File id of the logo"`

	// Nav is the navigation, in the order it is shown. There is no Order field
	// on an item for the same reason module.NavEntry has none: the order is the
	// order somebody wrote them in.
	Nav Nav `json:"nav,omitempty" gorm:"type:jsonb;not null;default:'[]'" required:"false" doc:"The site's navigation, in order"`

	// Revision counts this row's own writes, from 1 (migrations/000039). Nothing
	// in this module reads it. It exists because somebody diffing these settings
	// needs a number to say "I made this against revision 3", and the only honest
	// answer to "has it moved" is the row counting its own writes — see
	// WriteGate and the subject binding apps/platformkit writes for it.
	// readOnly because the count belongs to the row: a body that sent one would be
	// a caller choosing how many times it had been saved.
	Revision int64 `json:"revision" ui:"visibility:hidden" readOnly:"true" required:"false" doc:"This row's own write count, from 1"`
}

// LockedReader is the one read a caller about to write needs: the settings with
// the row locked, so the revision it is comparing against cannot move underneath
// it between the read and the write.
//
// It is a separate interface rather than a fourth method on Service because adding
// a method to an exported interface is a break to anything outside this repository
// that implements Service, and the exported-API gate refuses that break rather than
// treating it as a detail. The implementation is the same object either way: the
// composition puts one service under both interfaces, so the split costs a
// declaration and buys an unchanged contract. It is not a second constructor — a
// value built outside the wiring cannot reach what the wiring gives the service,
// and a locked read that resolved a different colour from the unlocked one would
// be the quiet half of the defect this contract closes.
type LockedReader interface {
	// SettingsForUpdate is Settings with the tenant's row locked FOR UPDATE. A
	// tenant with no row yet gets the defaults, whose revision is 0.
	SettingsForUpdate(ctx context.Context, tx db.Tx[db.Tenant]) (*SiteSettings, error)
}

// WriteGate is the question a deployment asks about a direct write of a tenant's
// settings: write them now, or not until somebody else has said yes.
//
// The question is asked here, in the module that owns the row, and answered
// somewhere else, because which writes are sensitive enough to need a second
// account is a fact about the installation and not about a site. The
// composition answers it — a flag read for the tenant, and a refusal naming the
// door to go through instead — and a module that read the flag itself would be a
// module with a product in it.
type WriteGate interface {
	// Check returns nil when the write may go ahead, or an error that names what
	// the caller has to do instead. A non-nil error writes nothing.
	Check(ctx context.Context, tx db.Tx[db.Tenant]) error
}

// TableName pins the table, so the entity and migrations/000018 agree.
func (SiteSettings) TableName() string { return "site_settings" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It normalises as well as refuses.
//
// Each refusal is a fault.Reason — verdict, source sentence, and the key this
// module's catalogue keeps its translation under — because the person reading a
// refused save speaks a language this file does not know. A Reason unwraps to
// ErrInvalid, so errors.Is, kit/rest's 422 and this sentence in a log or a problem
// document are what they were; only a page asks the catalogue.
func (s *SiteSettings) Validate(context.Context) error {
	s.Title = strings.TrimSpace(s.Title)
	s.Tagline = strings.TrimSpace(s.Tagline)
	s.HomeSlug = strings.ToLower(strings.TrimSpace(s.HomeSlug))
	s.PrimaryColor = strings.ToLower(strings.TrimSpace(s.PrimaryColor))
	if s.Theme == "" {
		s.Theme = ThemeSystem
	}
	// The empty colour is filled in by nothing: it is the stored answer to "did
	// anybody choose an accent", and the read resolves it against the installation
	// (Palette). Every guard below therefore measures a colour somebody did type.
	switch {
	// Characters, not bytes, here and below: len() counts bytes, so a title of
	// 60 Chinese characters used to be refused as 180. The same correction is
	// in modules/content, modules/billing and modules/file.
	case utf8.RuneCountInString(s.Title) > MaxTitle:
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.title.length",
			Text: "a title is at most %d characters", Args: []any{MaxTitle}}
	case utf8.RuneCountInString(s.Tagline) > MaxTagline:
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.tagline.length",
			Text: "a tagline is at most %d characters", Args: []any{MaxTagline}}
	// The length first, because the column is varchar(200) and a slug longer
	// than it is the difference between a 422 that says so and a 500 from the
	// database — which is why MaxHomeSlug is read here and not only declared
	// beside the other four bounds.
	case utf8.RuneCountInString(s.HomeSlug) > MaxHomeSlug:
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.homeSlug.length",
			Text: "a home slug is at most %d characters, and that one is %d",
			Args: []any{MaxHomeSlug, utf8.RuneCountInString(s.HomeSlug)}}
	case s.HomeSlug != "" && !slug.MatchString(s.HomeSlug):
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.homeSlug.form",
			Text: "%q is not a slug, so nothing can be served at it", Args: []any{s.HomeSlug}}
	case !slices.Contains(themes, s.Theme):
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.theme",
			Text: "theme %q is not one of %v", Args: []any{s.Theme, themes}}
	case s.PrimaryColor != "" && !hexColor.MatchString(s.PrimaryColor):
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.primaryColor",
			Text: "%q is not a colour; a primary colour is #rrggbb", Args: []any{s.PrimaryColor}}
	// The accent is drawn on the kit's two canvases, and a colour that vanishes
	// into one of them is a site whose buttons nobody can find. Both canvases,
	// whichever theme this tenant chose: `system` means the visitor may be on
	// either, and a rule that branched on the theme would let a light-only colour
	// ride into a site that follows the operating system into the dark one.
	//
	// Refused, never adjusted: an adjusted colour would make the stored row and the
	// served one disagree, would put a value nobody typed into the diff the trail
	// keeps forever, and would leave the generated form's `pattern` saying
	// `#rrggbb` while the truth were narrower. The sentence names both ratios and
	// both canvases, because the person on the other end of it can pick another
	// colour and that is the whole of the fix.
	case s.PrimaryColor != "" && !readsOnBothCanvases(s.PrimaryColor):
		light, dark, _ := AccentRatios(s.PrimaryColor)
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.primaryColor.contrast",
			Text: "%q reads %.2f:1 against the light canvas and %.2f:1 against the dark one; an accent must read at least %.1f:1 against both (%s and %s)",
			Args: []any{s.PrimaryColor, light, dark, MinAccentRatio, CanvasLight, CanvasDark}}
	case len(s.Nav) > MaxNav:
		return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.nav.count",
			Text: "a navigation is at most %d links", Args: []any{MaxNav}}
	}
	for i := range s.Nav {
		item := &s.Nav[i]
		item.Label, item.Path = strings.TrimSpace(item.Label), strings.TrimSpace(item.Path)
		switch {
		case item.Label == "" || utf8.RuneCountInString(item.Label) > MaxLabel:
			return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.nav.label",
				Text: "a link needs a label of at most %d characters, and %q is not one",
				Args: []any{MaxLabel, item.Label}}
		case !httpx.LocalPath(item.Path) || utf8.RuneCountInString(item.Path) > MaxPath:
			return &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.site.settings.nav.path",
				Text: "a link points at a path within this site, and %q is not one",
				Args: []any{item.Path}}
		}
	}
	return nil
}

// Public is what an anonymous visitor may read: the name, the navigation and
// the colour scheme. The rest — the home slug, the logo, the timestamps — is
// either an internal reference or nobody's business, and a public response that
// carried the whole row would be an admin screen anybody could read.
type Public struct {
	Title string `json:"title"`
	Nav   Nav    `json:"nav"`
	Theme string `json:"theme"`
}

// Service is the tenant's site settings: one read and one write, because a
// singleton has no list, no create and no delete.
//
// Both take the caller's transaction rather than opening one, so the change and
// its event commit together. The error a caller can act on is crud.ErrInvalid,
// from the entity's own Validate.
type Service interface {
	// Settings is what this tenant has configured, and the defaults when it has
	// configured nothing. It never reports "not found": every tenant has a
	// site, whether or not anybody has saved anything about it.
	//
	// The colour it answers is the resolved one: a tenant that named none is
	// answered the installation's accent for its mode (Palette), so a reader never
	// sees the stored empty colour. Saving that answer back stores the empty colour
	// again, which is what makes the read-then-save round trip change nothing.
	Settings(ctx context.Context, tx db.Tx[db.Tenant]) (*SiteSettings, error)

	// Save writes the settings and publishes site.settings_updated. Saving what
	// is already stored changes nothing and says nothing, so a screen that
	// submits its form twice does not invalidate a cache twice.
	//
	// A colour that repeats the installation's own is stored as the absence of one
	// (SiteSettings.ClearInstallationAccent), and the row returned carries the
	// resolved colour, so echoing a response back changes nothing either. What is
	// stored, and what the event and the trail report, is the stored value: the
	// installation's accent is nobody's edit.
	//
	// A body that names no colour is that absence as well. The published pattern
	// admits no empty string, so the empty colour arrives only as a field nobody
	// sent: a PUT that omits `primaryColor` returns a site that had chosen one to
	// the installation's accent, at the revision that PUT moves. A caller that means
	// to keep a colour sends it. Whether that door stays open on PUT is the
	// composition's — see the module README's Limits.
	//
	// It is the door an approved proposal comes through, and it is not gated: the
	// gate in Deps stands on the route a person types into, not here. Refusing the
	// apply as well would make a proposal impossible to apply.
	Save(ctx context.Context, tx db.Tx[db.Tenant], in *SiteSettings) (*SiteSettings, error)
}
