// Package internal is the site's pages: the home, one published page, and
// the two things a visitor sees when there is nothing to show.
package internal

import (
	"cmp"
	"context"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/css"
	"github.com/septagon-oss/platformkit/ui/page"
)

const (
	// assetPrefix is this module's own static tree, which the composition
	// resolves against the public surface: /web/assets, at every host, outside
	// every middleware chain.
	assetPrefix = "/web/assets"
	brand       = "PlatformKit"
	// sourceLanguage is the language this module's copy is written in, said the
	// way document.View.Language says it: the bar, the footer and the two empty
	// states are Go strings here, and the tenant's own title, tagline, nav and
	// page bodies arrive as they were authored. Composing Deps.Messages is what
	// lets a refusal of this site answer in the reader's language; it says nothing
	// about these words, and a page that declared Portuguese over them would be
	// telling a screen reader to read English with a Portuguese voice — the same
	// mistake modules/admin's dashboard made and unmade.
	sourceLanguage = "en"
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
	Files    richtext.Files
	Theme    design.Pair
	// SignIn and File are addresses this site links and does not own: the
	// composition supplies both, for the reason Deps names them.
	SignIn string
	File   func(id string) string
	// Messages is the composition's merged catalogue, named by web.Deps. Nil keeps
	// the site monolingual, as it was before any shell here was translated.
	Messages page.Messages
	// Translations and ContentRows are the pair web.Deps names: the port that holds
	// a page's other languages, and the content entity's own row set as that port
	// knows them. Both, or neither — see Site.serve.
	Translations rest.Translations
	ContentRows  rest.TranslationSource
}

// Mount composes the stylesheet once and serves the two routes through the
// composition layer. There are no controllers: the site runs no script.
func Mount(surfaces httpx.Surfaces, s Site) {
	sheet := ui.Compose(s.Theme, ui.Extra{Lists: lists(), Sheets: []*css.Sheet{prose()}})
	// The root claim. One module answers a tenant's host at "/", and Home says
	// whether this one took it: a product with a storefront of its own is
	// composed instead of this module, and a second claimant would be a boot
	// failure rather than a coin toss decided by the order of a list.
	home, _ := surfaces.Public.Home()
	home.Static(assetPrefix, ui.Assets(sheet))
	shell := page.Shell{
		Chrome:    page.Chrome{Brand: brand, Assets: home.PagePath(assetPrefix), Stylesheet: sheet},
		Frame:     frame,
		Tag:       "web",
		Back:      "/",
		BackLabel: "Back to the site",
		Messages:  s.Messages,
	}
	page.Serve(home, shell, page.Route{ID: "web-home", Method: http.MethodGet, Path: "/",
		Summary: "The site's home page", Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable}}, httpx.Public(), s.home)
	page.Serve(home, shell, page.Route{ID: "web-page", Method: http.MethodGet, Path: "/{slug}",
		Summary: "One published page", Errors: []int{http.StatusNotFound, http.StatusServiceUnavailable}}, httpx.Public(), s.page)
}

type slugInput struct {
	Slug string `path:"slug" maxLength:"200" doc:"The page's slug"`
	// Lang is the language this reader asked to be answered in. It is the
	// kernel's one contract (`?lang=`, the same parameter the generated reads
	// answer) and the address every hreflang alternate this page links points at,
	// so a page that ignored it would link addresses that do not serve what they
	// promise. A tag the tenant is not served in is ignored rather than honoured:
	// rest.ServeTranslated answers the page as it was authored, and the document
	// declares the language of the text it actually prints.
	Lang string `query:"lang" maxLength:"35" doc:"Answer in this language if this page is written in it; the tenant's own language, or none, is the page as it was authored"`
}

// homeInput is the root claim: the same question, on an address with no slug to
// carry it.
type homeInput struct {
	Lang string `query:"lang" maxLength:"35" doc:"Answer in this language if the home page is written in it"`
}

// home is the content the settings name as the home slug, or an honest empty
// state: a fresh installation has a site before it has a page.
func (s Site) home(ctx context.Context, r page.Request, in *homeInput) (page.View, error) {
	settings, tx, err := s.settings(ctx)
	if err != nil {
		return page.View{}, err
	}
	if settings.HomeSlug == "" {
		return s.view(settings, r, "Welcome", sourceLanguage, s.nothingYet()), nil
	}
	c, err := s.Content.Public(ctx, tx, settings.HomeSlug)
	if errors.Is(err, crud.ErrNotFound) {
		return s.view(settings, r, "Welcome", sourceLanguage, s.notPublished(settings.HomeSlug)), nil
	}
	if err != nil {
		return page.View{}, err
	}
	return s.article(ctx, tx, settings, r, c, "/", in.Lang)
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
	return s.article(ctx, tx, settings, r, c, "/"+in.Slug, in.Lang)
}

// serve is one page's text in the language its reader was answered in, and the
// languages this page exists in besides. Which draft may be read and which locale
// counts as an answer are the kernel's rules, not this module's:
// rest.ServeTranslated runs the same port call the `?lang=` read doors run, with
// the same withholding of an unreviewed machine draft. What is this module's own
// is the pair it hands over, and the fact that a site with no translation module
// composed serves the page as it was authored, in the tenant's own language, and
// links no alternate.
func (s Site) serve(ctx context.Context, tx db.Tx[db.Tenant], c *contentcontracts.Content,
	want string) (rest.Serving, error) {
	if s.Translations == nil || s.ContentRows == nil {
		// No language named and no locale complete: the page is the text it was
		// authored in, declares the language this module's own copy is written in,
		// and links no alternate, because there is no other language to link.
		return rest.Serving{Values: map[string]string{
			contentcontracts.FieldTitle: c.Title, contentcontracts.FieldBody: c.Body,
		}}, nil
	}
	return rest.ServeTranslated(ctx, tx, s.ContentRows, s.Translations, c.ID, want)
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

func (s Site) article(ctx context.Context, tx db.Tx[db.Tenant], settings *sitecontracts.SiteSettings, r page.Request,
	c *contentcontracts.Content, address, asked string) (page.View, error) {
	serving, err := s.serve(ctx, tx, c, wantedLanguage(r, asked))
	if err != nil {
		return page.View{}, err
	}
	// An empty Serving.Language is the one case rest.ServeTranslated never answers:
	// the site with no translation module composed, whose pages are in the language
	// this module's own copy is written in.
	language := cmp.Or(serving.Language, sourceLanguage)
	title := serving.Values[contentcontracts.FieldTitle]
	doc, err := richtext.Parse(serving.Values[contentcontracts.FieldBody])
	if err != nil {
		return page.View{}, err
	}
	html, err := richtext.Render(ctx, tx, doc, s.Files, richtext.Public)
	if err != nil {
		return page.View{}, err
	}
	v := s.view(settings, r, title, language, []g.Node{h.Article(
		components.Heading(components.HeadingProps{Text: title, Level: 1}),
		h.Div(g.Attr("data-prose", ""), components.Prose(components.ProseProps{HTML: html})))})
	// The alternates come before the description, in the order a reader of the
	// head meets them: what languages this page stands in, then what it says.
	v.Head = append(v.Head, alternates(address, serving)...)
	v.Head = append(v.Head, h.Meta(h.Name("description"), h.Content(richtext.MetaDescription(doc, 160))))
	if first, ok := richtext.FirstImage(doc); ok && s.Files != nil {
		if image, err := s.Files.Resolve(ctx, tx, first.ID, richtext.Public); err == nil {
			v.Head = append(v.Head, h.Meta(g.Attr("property", "og:image"), h.Content(image.Src)))
		}
	}
	return v, nil
}

// wantedLanguage is the language this reader asked to be answered in: what they
// said for this one request (`?lang=`, the address every alternate on this page
// points at), and otherwise what the shell negotiated from the cookie the
// workspace left and the browser's own list. Whether the answer is a language this
// tenant serves is the read's question, not this one.
func wantedLanguage(r page.Request, asked string) string {
	if asked != "" {
		return asked
	}
	if r.Locale != nil {
		return r.Locale.Language
	}
	return ""
}

// alternates is the head a search engine reads: one address per language this page
// is written in, and an x-default for the address a visitor with no stated
// preference is given — which is the page as it was authored, at the bare path.
//
// The source language carries the bare address too; every other locale is reached
// at `?lang=`, because a reader who followed an alternate and was answered the
// source would be a reader the page lied to. An outdated translation and an
// unreviewed draft are absent from the list, which is the whole reason the list is
// derived rather than typed: an alternate that promises a language the page is not
// complete in is a promise a crawler holds for a year.
func alternates(address string, serving rest.Serving) []g.Node {
	if len(serving.Complete) == 0 {
		return nil
	}
	var out []g.Node
	for _, tag := range serving.Complete {
		href := address
		if tag != serving.Complete[0] {
			href = languageURL(address, tag)
		}
		out = append(out, h.Link(h.Rel("alternate"), g.Attr("hreflang", tag), h.Href(href)))
	}
	return append(out, h.Link(h.Rel("alternate"), g.Attr("hreflang", "x-default"), h.Href(address)))
}

// languageURL puts the language question on one address, whichever shape that
// address arrived in.
func languageURL(address, tag string) string {
	q := url.Values{"lang": []string{tag}}.Encode()
	if strings.Contains(address, "?") {
		return address + "&" + q
	}
	return address + "?" + q
}

// view is every page of the site: the bar, the column, the footer, and the
// tenant's theme and colour pinned on the document. Revalidate is set because an
// owner publishes over the same address: the minute a public page may be kept is
// otherwise the minute in which their own publish looks lost — e2e/site.spec.ts is
// that journey, and it failed until this line.
//
// language is the tag of the copy in main — the page's own text in whatever
// language the reader was answered, or sourceLanguage for the copy this module
// writes for itself — and it is what `<html lang>` declares. The bar and the
// footer are Go text here, so when the document declares another language they
// carry their own: the same rule modules/admin's dashboard learned the hard way,
// and the one a nested lang attribute exists for.
func (s Site) view(settings *sitecontracts.SiteSettings, r page.Request, title, language string, main []g.Node) page.View {
	v := page.View{Title: title, Revalidate: true, Language: language, Body: []g.Node{
		s.header(settings, r, language),
		h.Main(h.ID("content"), h.Class(clMain.Compile()),
			components.Container(components.ContainerProps{MaxWidth: "3xl"}, main...)),
		footer(settings, r, language),
	}}
	if settings.Theme == "light" || settings.Theme == "dark" {
		v.Theme = settings.Theme
	}
	// The tenant's accent is the one style this repository writes outside
	// ui.Compose: the colour is a row, not a Go sheet, so no layer Compose emits
	// can carry it. Unlayered is what lets a tenant palette outrank every layer,
	// which is the point of a per-tenant colour, and the guard is the pattern —
	// an inline declaration reaches the gate that refuses a consumer sheet a raw
	// colour and the --pk- namespace. See refuseClientSheet.
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

func (s Site) header(settings *sitecontracts.SiteSettings, r page.Request, language string) g.Node {
	var mark []g.Node
	if settings.LogoFileID != nil {
		mark = append(mark, h.Img(h.Class(clLogo.Compile()), h.Src(s.File(settings.LogoFileID.String())), h.Alt("")))
	}
	mark = append(mark, h.A(h.Href("/"), h.Class(clTitle.Compile()), g.Text(name(settings, r))))
	if settings.Tagline != "" {
		mark = append(mark, components.Text(components.TextProps{Content: settings.Tagline, Size: "sm", Color: "muted"}))
	}
	links := make([]g.Node, 0, len(settings.Nav))
	for _, item := range settings.Nav {
		links = append(links, components.Link(components.LinkProps{Label: item.Label, Href: item.Path}))
	}
	return h.Header(h.Class(clHeader.Compile()), foreign(language),
		h.Div(h.Class(clBrand.Compile()), g.Group(mark)),
		h.Nav(h.Class(clNav.Compile()), g.Attr("aria-label", "Site navigation"), g.Group(links)))
}

// foreign marks the site's own copy as the language it is written in for the one
// case that needs marking: a page whose content was answered in another language.
// When the two agree — every page before a translation exists, and every page of
// a tenant served in one language — it adds nothing.
func foreign(language string) g.Node {
	if language == "" || language == sourceLanguage {
		return g.Raw("")
	}
	return h.Lang(sourceLanguage)
}

func footer(settings *sitecontracts.SiteSettings, r page.Request, language string) g.Node {
	return h.Footer(h.Class(clFooter.Compile()), foreign(language),
		h.Div(h.Class(clFooterCopy.Compile()),
			components.Text(components.TextProps{Content: name(settings, r) + " · " + brand, Size: "xs", Color: "muted"})))
}

func (s Site) nothingYet() []g.Node {
	return []g.Node{
		components.Heading(components.HeadingProps{Text: "Nothing published yet", Level: 1}),
		components.Text(components.TextProps{Content: "This site has no home page. Sign in to the admin, publish a page, and name its slug as the site's home slug."}),
		components.Link(components.LinkProps{Label: "Sign in to the admin", Href: s.SignIn}),
	}
}

func (s Site) notPublished(homeSlug string) []g.Node {
	return []g.Node{
		components.Heading(components.HeadingProps{Text: "The home page is not published", Level: 1}),
		components.Text(components.TextProps{Content: "The site's home slug is " + homeSlug + ", and nothing published has that slug."}),
		components.Link(components.LinkProps{Label: "Sign in to the admin", Href: s.SignIn}),
	}
}
