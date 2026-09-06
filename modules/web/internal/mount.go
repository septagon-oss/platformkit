// Package internal is the site's pages: the home, one published page, and
// the two things a visitor sees when there is nothing to show.
package internal

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/page"
)

const (
	assetPrefix = "/web/assets"
	signInPath  = "/admin/login"
	// filePath is where the file module serves a public file; the logo is one.
	filePath = "/api/v1/file/public/"
	brand    = "PlatformKit"
)

// slug is the content module's own grammar for a slug, so a path that is not
// one is a 404 here rather than a validation error about a path parameter.
var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// colour is the site module's own grammar for a brand colour, checked again
// here because the value is written into a style element.
var colour = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Site is the shell: the two services it reads and the palette it composes.
type Site struct {
	Settings sitecontracts.Service
	Content  contentcontracts.Service
	Theme    design.Pair
}

// Mount composes the stylesheet once and serves the two routes through the
// composition layer. There are no controllers: the site runs no script.
func Mount(api *httpx.API, s Site) {
	sheet := ui.Compose(s.Theme, ui.Extra{Lists: lists(), Sheets: []*css.Sheet{prose()}})
	api.Static(assetPrefix, ui.Assets(sheet))
	shell := page.Shell{
		Chrome:    page.Chrome{Brand: brand, Assets: assetPrefix, Stylesheet: sheet},
		Frame:     frame,
		Tag:       "web",
		Back:      "/",
		BackLabel: "Back to the site",
	}
	page.Serve(api, shell, page.Route{ID: "web-home", Method: http.MethodGet, Path: "/",
		Summary: "The site's home page", Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable}}, httpx.Public(), s.home)
	page.Serve(api, shell, page.Route{ID: "web-page", Method: http.MethodGet, Path: "/{slug}",
		Summary: "One published page", Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable}}, httpx.Public(), s.page)
}

type slugInput struct {
	Slug string `path:"slug" maxLength:"200" doc:"The page's slug"`
}

// home is the content the settings name as the home slug, or an honest empty
// state: a fresh installation has a site before it has a page.
func (s Site) home(ctx context.Context, r page.Request, _ *page.Empty) (page.View, error) {
	settings, tx, err := s.settings(ctx)
	if err != nil {
		return page.View{}, err
	}
	if settings.HomeSlug == "" {
		return s.view(settings, r, "Welcome", nothingYet()), nil
	}
	c, err := s.Content.Public(ctx, tx, settings.HomeSlug)
	if errors.Is(err, crud.ErrNotFound) {
		return s.view(settings, r, "Welcome", notPublished(settings.HomeSlug)), nil
	}
	if err != nil {
		return page.View{}, err
	}
	return s.article(settings, r, c)
}

// page is one published page by slug. A draft, an archived page and a slug
// nobody has used are the same 404, which is the content module's rule.
func (s Site) page(ctx context.Context, r page.Request, in *slugInput) (page.View, error) {
	if !slug.MatchString(in.Slug) {
		return page.View{}, problem.NotFound("there is no page at /" + in.Slug)
	}
	settings, tx, err := s.settings(ctx)
	if err != nil {
		return page.View{}, err
	}
	c, err := s.Content.Public(ctx, tx, in.Slug)
	if errors.Is(err, crud.ErrNotFound) {
		return page.View{}, problem.NotFound("there is no page at /" + in.Slug)
	}
	if err != nil {
		return page.View{}, err
	}
	return s.article(settings, r, c)
}

// settings reads the tenant's settings for this request. A host that resolves
// to no tenant serves no site, and a database that cannot be reached is said
// so rather than shown as an empty site.
func (s Site) settings(ctx context.Context) (*sitecontracts.SiteSettings, db.Tx[db.Tenant], error) {
	if _, ok := tenancy.FromContext(ctx); !ok {
		return nil, db.Tx[db.Tenant]{}, problem.NotFound("no site is served at this host")
	}
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return nil, db.Tx[db.Tenant]{}, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	settings, err := s.Settings.Settings(ctx, tx)
	if err != nil {
		return nil, db.Tx[db.Tenant]{}, err
	}
	return settings, tx, nil
}

func (s Site) article(settings *sitecontracts.SiteSettings, r page.Request, c *contentcontracts.Content) (page.View, error) {
	html, err := contentcontracts.Render(c.Body)
	if err != nil {
		return page.View{}, err
	}
	return s.view(settings, r, c.Title, []g.Node{h.Article(g.Attr("data-prose", ""),
		components.Heading(components.HeadingProps{Text: c.Title, Level: 1}),
		g.Raw(html))}), nil
}

// view is every page of the site: the bar, the column, the footer, and the
// tenant's theme and colour pinned on the document.
func (s Site) view(settings *sitecontracts.SiteSettings, r page.Request, title string, main []g.Node) page.View {
	v := page.View{Title: title, Body: []g.Node{
		header(settings, r),
		h.Main(h.ID("content"), h.Class(clMain.Compile()),
			components.Container(components.ContainerProps{MaxWidth: "3xl"}, main...)),
		footer(settings, r),
	}}
	if settings.Theme == "light" || settings.Theme == "dark" {
		v.Theme = settings.Theme
	}
	if colour.MatchString(settings.PrimaryColor) {
		v.Head = []g.Node{h.StyleEl(g.Raw(":root{--pk-color-accent-default:" + settings.PrimaryColor + "}"))}
	}
	return v
}

func frame(_ context.Context, _ page.Request, body []g.Node) g.Node {
	return h.Div(h.Class(clPage.Compile()), g.Group(body))
}

// name is what the site calls itself: its title, else the tenant's name, else
// the installation's.
func name(settings *sitecontracts.SiteSettings, r page.Request) string {
	switch {
	case settings.Title != "":
		return settings.Title
	case r.Tenant.Name != "":
		return r.Tenant.Name
	}
	return brand
}

func header(settings *sitecontracts.SiteSettings, r page.Request) g.Node {
	var mark []g.Node
	if settings.LogoFileID != nil {
		mark = append(mark, h.Img(h.Class(clLogo.Compile()), h.Src(filePath+settings.LogoFileID.String()), h.Alt("")))
	}
	mark = append(mark, h.A(h.Href("/"), h.Class(clTitle.Compile()), g.Text(name(settings, r))))
	if settings.Tagline != "" {
		mark = append(mark, components.Text(components.TextProps{Content: settings.Tagline, Size: "sm", Color: "muted"}))
	}
	links := make([]g.Node, 0, len(settings.Nav))
	for _, item := range settings.Nav {
		links = append(links, components.Link(components.LinkProps{Label: item.Label, Href: item.Path}))
	}
	return h.Header(h.Class(clHeader.Compile()),
		h.Div(h.Class(clBrand.Compile()), g.Group(mark)),
		h.Nav(h.Class(clNav.Compile()), g.Attr("aria-label", "Site navigation"), g.Group(links)))
}

func footer(settings *sitecontracts.SiteSettings, r page.Request) g.Node {
	return h.Footer(h.Class(clFooter.Compile()),
		components.Text(components.TextProps{Content: name(settings, r) + " · " + brand, Size: "xs", Color: "muted"}))
}

func nothingYet() []g.Node {
	return []g.Node{
		components.Heading(components.HeadingProps{Text: "Nothing published yet", Level: 1}),
		components.Text(components.TextProps{Content: "This site has no home page. Sign in to the admin, publish a page, and name its slug as the site's home slug."}),
		components.Link(components.LinkProps{Label: "Sign in to the admin", Href: signInPath}),
	}
}

func notPublished(homeSlug string) []g.Node {
	return []g.Node{
		components.Heading(components.HeadingProps{Text: "The home page is not published", Level: 1}),
		components.Text(components.TextProps{Content: "The site's home slug is " + homeSlug + ", and nothing published has that slug."}),
		components.Link(components.LinkProps{Label: "Sign in to the admin", Href: signInPath}),
	}
}
