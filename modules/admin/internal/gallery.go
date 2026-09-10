package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"

	"github.com/danielgtaylor/huma/v2"
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/page"
)

type galleryInput struct {
	Group   string `query:"group" maxLength:"100"`
	Example string `query:"example" maxLength:"250"`
	Props   string `query:"props" maxLength:"12000"`
	Theme   string `query:"theme" default:"light" enum:"light,dark,system"`
	Width   string `query:"width" default:"fit" enum:"fit,320,768,1280"`
}

// storybook is the only selection path, shared by index, previews, exports and
// navigation. A configured provider is authoritative even for operator users.
func (s Shell) storybook(ctx context.Context) (ui.Storybook, error) {
	tenant, ok := tenancy.FromContext(ctx)
	if !ok {
		return ui.Storybook{}, problem.New(http.StatusForbidden, "No storybook is available.")
	}
	var book ui.Storybook
	if s.Storybook != nil {
		var err error
		book, err = s.Storybook(ctx)
		if err != nil {
			return ui.Storybook{}, err
		}
	} else if tenant.Operator {
		book = ui.Storybook{Title: "Components", Theme: s.Theme, Examples: components.Gallery()}
	} else {
		return ui.Storybook{}, problem.New(http.StatusForbidden, "No storybook is available for this tenant.")
	}
	if book.Theme == (design.Pair{}) {
		book.Theme = s.Theme
	}
	if book.Title == "" {
		book.Title = "Components"
	}
	return book, book.Validate()
}

func galleryExample(book ui.Storybook, in *galleryInput) (components.Example, error) {
	var example components.Example
	if in.Example != "" {
		var found bool
		example, found = book.Find(in.Example)
		if !found {
			return example, problem.New(http.StatusNotFound, "This example is not available.")
		}
	} else {
		for _, candidate := range book.Examples {
			if in.Group == "" || candidate.Group == in.Group {
				example = candidate
				break
			}
		}
	}
	if in.Props != "" && in.Props != "{}" && example.ID != "" {
		var err error
		example, err = example.WithProps(json.RawMessage(in.Props))
		if err != nil {
			return example, problem.New(http.StatusUnprocessableEntity, err.Error())
		}
	}
	return example, nil
}

func (p pages) mountGallery(api *httpx.API) {
	p.mountStorybook(api)
	auth := httpx.Permission("gallery:read")
	page.Serve(api, p.shell, page.Route{ID: "admin-gallery", Method: http.MethodGet, Path: galleryPath, Summary: "The tenant's component gallery"},
		auth, func(ctx context.Context, _ page.Request, in *galleryInput) (page.View, error) {
			book, err := p.storybook(ctx)
			if err != nil {
				return page.View{}, err
			}
			example, err := galleryExample(book, in)
			if err != nil {
				return page.View{}, err
			}
			body, err := ui.StorybookPage(book, galleryPath, in.Group, example, in.Props, in.Theme, in.Width)
			return page.View{Title: book.Title, Head: []g.Node{
				h.Link(h.Rel("stylesheet"), h.Href(assetPrefix+"/gallery.css?v="+ui.Gallery().Fingerprint)),
				h.StyleEl(g.Raw(ui.StorybookCSS())),
			}, Body: []g.Node{body}}, err
		})
	for _, route := range []string{"preview", "export"} {
		op := huma.Operation{OperationID: "admin-gallery-" + route, Method: http.MethodGet, Path: galleryPath + "/" + route, Tags: []string{"admin"}}
		httpx.SignIn(&op, loginPath)
		httpx.HTML(api, op, auth, func(ctx context.Context, in *galleryInput) (*httpx.Page, error) {
			book, err := p.storybook(ctx)
			if err != nil {
				return nil, err
			}
			if route == "export" {
				snapshot, err := ui.Export(book.Theme, book.Examples, book.Extra...)
				if err != nil {
					return nil, err
				}
				body, err := json.Marshal(snapshot)
				return &httpx.Page{Status: http.StatusOK, ContentType: "application/json", CacheControl: "no-store", Body: body}, err
			}
			if in.Example == "" {
				return nil, problem.New(http.StatusNotFound, "Choose an example to preview.")
			}
			example, err := galleryExample(book, in)
			if err != nil {
				return nil, err
			}
			return galleryPreview(book, example, in.Theme)
		})
	}
}

func galleryPreview(book ui.Storybook, example components.Example, mode string) (*httpx.Page, error) {
	extra := append(slices.Clone(book.Extra), ui.Extra{Lists: components.GalleryClassLists()})
	sheet := ui.Compose(book.Theme, extra...)
	attrs := []g.Node{h.Lang("en")}
	if mode != "system" {
		attrs = append(attrs, g.Attr("data-theme", mode))
	}
	attrs = append(attrs, h.Head(h.Meta(h.Charset("utf-8")), h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
		h.TitleEl(g.Text(example.Name)), h.StyleEl(g.Raw(string(sheet.Body))),
		h.Script(h.Src(assetPrefix+"/js/htmx.min.js"), g.Attr("defer")),
		h.Script(h.Src(assetPrefix+"/js/components.js"), g.Attr("defer")),
		h.Script(h.Src(assetPrefix+"/js/confirm.js"), g.Attr("defer")),
		h.Script(h.Src(assetPrefix+"/js/gallery-preview.js"), g.Attr("defer"))),
		h.Body(h.Main(h.Style("padding:1.5rem"), example.Node)))
	out, err := httpx.Document(h.HTML(attrs...), http.StatusOK)
	if err != nil {
		return nil, err
	}
	// Enforce isolation on the direct URL too. Only the widget scripts execute;
	// sample forms, HTMX requests and nested frames cannot reach application data.
	out.ContentSecurityPolicy = "sandbox allow-scripts; default-src 'none'; script-src 'self'; style-src 'unsafe-inline'; img-src 'self' data:; font-src 'self' data:; frame-ancestors 'self'; base-uri 'none'; form-action 'none'"
	out.FrameOptions = "SAMEORIGIN"
	out.CacheControl = "no-store"
	return out, nil
}
