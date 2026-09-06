package internal

import (
	"context"
	"net/http"
	"strconv"
	"strings"
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
		}, rest.CommandOptions{})

	rest.Command(api, spec, "unpublish",
		"Unpublish content", "Takes it back to a draft, from published or from archived, and clears the publication time.",
		[]string{contracts.EventUnpublished},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Content, error) {
			return svc.Unpublish(ctx, tx, id)
		}, rest.CommandOptions{})

	rest.Command(api, spec, "archive",
		"Archive content", "Keeps it and serves it to nobody. Archiving twice changes nothing.",
		[]string{contracts.EventArchived},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.Content, error) {
			return svc.Archive(ctx, tx, id)
		}, rest.CommandOptions{})

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
	}, httpx.Public(), func(ctx context.Context, in *slugInput) (*published, error) {
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
		// The row is read before the body is rendered, and the tag is the row's
		// own updated_at: rendering is the expensive half — the review measured
		// 2.25 seconds for one large page, on every anonymous request — and a
		// reader who already has the page must not pay for it again. A weak tag
		// because two responses with one updated_at are equivalent and not
		// necessarily byte-identical: the renderer's output may change with the
		// renderer.
		tag := `W/"` + strconv.FormatInt(c.UpdatedAt.UnixNano(), 36) + `"`
		if matches(in.IfNoneMatch, tag) {
			// 304, and nothing else: net/http does not send a body with one.
			return &published{Status: http.StatusNotModified, ETag: tag}, nil
		}
		html, err := contracts.Render(c.Body)
		if err != nil {
			return nil, rest.Fault(err)
		}
		return &published{Status: http.StatusOK, ETag: tag, Body: &Page{
			Slug: c.Slug, Title: c.Title, Kind: c.Kind, HTML: html, PublishedAt: *c.PublishedAt,
		}}, nil
	})
}

// published is the public route's response: the page, the tag a reader quotes
// back, and the status, which is the one thing a handler here decides for
// itself.
type published struct {
	Status int
	ETag   string `header:"ETag" doc:"Changes when the content does; quote it back in If-None-Match"`
	Body   *Page
}

// matches reports whether an If-None-Match header names tag. The header is a
// list, and a caller that has been through a proxy may send the weak form of a
// strong tag, so the comparison is the weak one RFC 9110 defines for it.
func matches(header, tag string) bool {
	if header == "" {
		return false
	}
	want := strings.TrimPrefix(tag, "W/")
	for candidate := range strings.SplitSeq(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || strings.TrimPrefix(candidate, "W/") == want {
			return true
		}
	}
	return false
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
	Slug        string `path:"slug" pattern:"^[a-z0-9]+(-[a-z0-9]+)*$" maxLength:"200" doc:"The content's slug"`
	IfNoneMatch string `header:"If-None-Match" required:"false" doc:"A tag from a previous response; a match is answered with 304 and no body"`
}
