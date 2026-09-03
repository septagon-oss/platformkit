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

// DefaultPrimaryColor is the colour a site that has chosen none is rendered in.
// It is here rather than in a theme because a theme may be replaced and this is
// what a tenant's settings say when they say nothing.
const DefaultPrimaryColor = "#2563eb"

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
	Title   string `json:"title,omitempty" gorm:"type:varchar(120);not null;default:''" maxLength:"120" doc:"The site's name" example:"Acme"`
	Tagline string `json:"tagline,omitempty" gorm:"type:varchar(200);not null;default:''" maxLength:"200" doc:"The line under the name" example:"We make things"`

	// HomeSlug is the content served at the site's root. It is a slug and not
	// an id because a page can be rewritten and replaced and still be the home
	// page; it is empty until somebody chooses one.
	HomeSlug string `json:"homeSlug,omitempty" gorm:"type:varchar(200);not null;default:''" maxLength:"200" doc:"Slug of the content served at /" example:"welcome"`

	// Theme and PrimaryColor are the whole of what a tenant may say about how
	// their site looks. A theme that needed more would be a theme with a
	// stylesheet editor in it.
	Theme        string `json:"theme" gorm:"type:varchar(10);not null;default:'system'" enum:"light,dark,system" ui:"widget:select" doc:"Colour scheme, or system to follow the visitor's" default:"system" required:"false"`
	PrimaryColor string `json:"primaryColor" gorm:"type:char(7);not null;default:'#2563eb'" pattern:"^#[0-9a-fA-F]{6}$" doc:"Brand colour, #rrggbb" default:"#2563eb" required:"false" example:"#2563eb"`

	// LogoFileID is a file id with no foreign key behind it, which is what
	// "cross-module dependencies are Go interfaces" costs at the database: this
	// module never names modules/file, and a logo that has been deleted is a
	// site that renders without one.
	LogoFileID *uuid.UUID `json:"logoFileId,omitempty" gorm:"type:uuid" format:"uuid" doc:"File id of the logo"`

	// Nav is the navigation, in the order it is shown. There is no Order field
	// on an item for the same reason module.NavEntry has none: the order is the
	// order somebody wrote them in.
	Nav Nav `json:"nav,omitempty" gorm:"type:jsonb;not null;default:'[]'" required:"false" doc:"The site's navigation, in order"`
}

// TableName pins the table, so the entity and migrations/000018 agree.
func (SiteSettings) TableName() string { return "site_settings" }

// Validate is the entity's own check, run by kit/crud on every write whichever
// door it came through. It normalises as well as refuses.
func (s *SiteSettings) Validate(context.Context) error {
	s.Title = strings.TrimSpace(s.Title)
	s.Tagline = strings.TrimSpace(s.Tagline)
	s.HomeSlug = strings.ToLower(strings.TrimSpace(s.HomeSlug))
	s.PrimaryColor = strings.ToLower(strings.TrimSpace(s.PrimaryColor))
	if s.Theme == "" {
		s.Theme = ThemeSystem
	}
	if s.PrimaryColor == "" {
		s.PrimaryColor = DefaultPrimaryColor
	}
	switch {
	// Characters, not bytes, here and below: len() counts bytes, so a title of
	// 60 Chinese characters used to be refused as 180. The same correction is
	// in modules/content, modules/billing and modules/file.
	case utf8.RuneCountInString(s.Title) > MaxTitle:
		return fmt.Errorf("a title is at most %d characters", MaxTitle)
	case utf8.RuneCountInString(s.Tagline) > MaxTagline:
		return fmt.Errorf("a tagline is at most %d characters", MaxTagline)
	case s.HomeSlug != "" && !slug.MatchString(s.HomeSlug):
		return fmt.Errorf("%q is not a slug, so nothing can be served at it", s.HomeSlug)
	case !slices.Contains(themes, s.Theme):
		return fmt.Errorf("theme %q is not one of %v", s.Theme, themes)
	case !hexColor.MatchString(s.PrimaryColor):
		return fmt.Errorf("%q is not a colour; a primary colour is #rrggbb", s.PrimaryColor)
	case len(s.Nav) > MaxNav:
		return fmt.Errorf("a navigation is at most %d links", MaxNav)
	}
	for i := range s.Nav {
		item := &s.Nav[i]
		item.Label, item.Path = strings.TrimSpace(item.Label), strings.TrimSpace(item.Path)
		switch {
		case item.Label == "" || utf8.RuneCountInString(item.Label) > MaxLabel:
			return fmt.Errorf("a link needs a label of at most %d characters, and %q is not one", MaxLabel, item.Label)
		case !httpx.LocalPath(item.Path) || utf8.RuneCountInString(item.Path) > MaxPath:
			return fmt.Errorf("a link points at a path within this site, and %q is not one", item.Path)
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
	Settings(ctx context.Context, tx db.Tx[db.Tenant]) (*SiteSettings, error)

	// Save writes the settings and publishes site.settings_updated. Saving what
	// is already stored changes nothing and says nothing, so a screen that
	// submits its form twice does not invalidate a cache twice.
	Save(ctx context.Context, tx db.Tx[db.Tenant], in *SiteSettings) (*SiteSettings, error)
}
