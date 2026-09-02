package internal

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
)

// PublicPath is where the site reads a page by name. It is a sibling of the
// Spec's collection rather than a route under it, so that "everything under
// /api/v1/content/contents needs content:read" stays true by looking.
const PublicPath = "/api/v1/content/public/{slug}"

// RegisterRoutes mounts the three lifecycle commands and the one public read.
//
// The commands are routes rather than fields of a PATCH because each is a rule
// about the state the content is in and each publishes an event: a caller who
// could write status="published" through the generic update would serve a page
// with no publication time and tell nobody. spec.Immutable is the other half of
// that argument.
func RegisterRoutes(api *httpx.API, spec rest.Spec[*contracts.Content], svc contracts.Service) {
	rest.Command(api, spec, "publish",
		"Publish content", "Serves it to anybody, and records when. Publishing what is already published changes nothing; the publication time does not move.",
		[]string{contracts.EventPublished},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Content, error) {
			return svc.Publish(ctx, tx, id)
		})

	rest.Command(api, spec, "unpublish",
		"Unpublish content", "Takes it back to a draft, from published or from archived, and clears the publication time.",
		[]string{contracts.EventUnpublished},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Content, error) {
			return svc.Unpublish(ctx, tx, id)
		})

	rest.Command(api, spec, "archive",
		"Archive content", "Keeps it and serves it to nobody. Archiving twice changes nothing.",
		[]string{contracts.EventArchived},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Content, error) {
			return svc.Archive(ctx, tx, id)
		})

	// The public site's one route. It is Public because the whole point of
	// publishing is that a reader does not sign in, and it is safe to be
	// because the tenant still comes from the request's own host and the query
	// still runs under that tenant's policy: an anonymous caller reads one
	// tenant's published rows and there is no parameter that could widen it.
	httpx.Register(api, huma.Operation{
		OperationID: "content-content-public",
		Method:      http.MethodGet,
		Path:        PublicPath,
		Summary:     "Read published content by slug",
		Description: "The rendered, sanitized HTML of one published page or post. A draft, an archived page and a slug nobody has used are the same 404.",
		Tags:        []string{"content"},
		Errors:      []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, httpx.Public(), func(ctx context.Context, in *slugInput) (*rest.Item[*Page], error) {
		if _, ok := tenancy.FromContext(ctx); !ok {
			// A public route is reached at hosts that resolve to no tenant —
			// a probe addressing the pod — and there is no site there to read.
			return nil, problem.NotFound("no site is served at this host")
		}
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		c, err := svc.Public(ctx, tx, in.Slug)
		if err != nil {
			return nil, rest.Fault(err)
		}
		html, err := Render(c.Body)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &rest.Item[*Page]{Body: &Page{
			Slug: c.Slug, Title: c.Title, Kind: c.Kind, HTML: html, PublishedAt: *c.PublishedAt,
		}}, nil
	})
}

// Page is what the public route answers with: the content as a reader needs it,
// and nothing else. The body is HTML and not Markdown because a reader has no
// renderer, and the author, the timestamps and the status are not a reader's
// business — a public response that carried the whole entity would be an admin
// screen anybody could read.
type Page struct {
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Kind        string    `json:"kind"`
	HTML        string    `json:"html" doc:"The body, rendered from Markdown and sanitized"`
	PublishedAt time.Time `json:"publishedAt"`
}

// slugInput is the public route's path parameter. The pattern is the grammar
// contracts.Slugify produces, so a name that could never be stored is a 422 at
// the door rather than a query that finds nothing.
type slugInput struct {
	Slug string `path:"slug" pattern:"^[a-z0-9]+(-[a-z0-9]+)*$" maxLength:"200" doc:"The content's slug"`
}
