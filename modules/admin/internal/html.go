// Package internal is the shell's implementation: the frame, the pages written
// by hand, and the screens generated from every resource kit/rest registered.
package internal

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Shell is what the manifest hands the implementation.
type Shell struct {
	Nav       []module.NavEntry
	Checks    []health.Check
	Authorize httpx.Authorizer
	Tenants   tenantcontracts.Service
	Token     tenancy.SystemToken
	// generated are the paths the screens took, so the sidebar can tell a link
	// that leads somewhere from one that does not; operatorOnly are the ones
	// only the operator's own tenant may follow.
	generated    map[string]bool
	operatorOnly map[string]bool
}

// page is an HTML response. huma writes a []byte body verbatim, so this is the
// whole of "serve HTML" — no second router, no second middleware chain, and the
// same recording, the same authorization declaration and the same transaction
// as every JSON route in the application.
type page struct {
	Status      int
	ContentType string `header:"Content-Type"`
	Location    string `header:"Location"`
	HXRedirect  string `header:"HX-Redirect"`
	Body        []byte
}

// document renders a node as a whole HTML document.
func document(node g.Node, status int) (*page, error) {
	var b strings.Builder
	b.WriteString("<!doctype html>")
	if err := node.Render(&b); err != nil {
		return nil, err
	}
	return &page{Status: status, ContentType: "text/html; charset=utf-8", Body: []byte(b.String())}, nil
}

// redirect sends the caller somewhere else after a write. htmx does not follow
// a 303 the way a browser does — it would swap the target page into a fragment
// — so a request it made is answered with the header it understands instead.
func redirect(ctx context.Context, to string) *page {
	if r, ok := httpx.RequestFrom(ctx); ok && r.Header.Get("HX-Request") == "true" {
		return &page{Status: http.StatusNoContent, HXRedirect: to}
	}
	return &page{Status: http.StatusSeeOther, Location: to}
}

// html mounts one HTML route, with the authorization it declares, exactly like
// every other operation in the application. Hidden keeps it out of the OpenAPI
// document — a page is not an API — and the kernel records it anyway, so the
// boot gate still sees it. See kit/httpx's package comment.
//
// A handler answers with a page or with an error. seeOther is the third
// answer, and it is an error because "not this page" is a return a handler
// already has: keeping it there is what lets every screen have one signature.
func html[I any](api *httpx.API, s *Shell, id, method, path, summary string, auth httpx.Auth,
	handler func(context.Context, *I) (*page, error),
) {
	httpx.Register(api, huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"admin"}, Hidden: true,
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}, auth, func(ctx context.Context, in *I) (*page, error) {
		out, err := handler(ctx, in)
		var to seeOther
		switch {
		case errors.As(err, &to):
			return redirect(ctx, string(to)), nil
		case err != nil:
			return s.fault(ctx, err)
		}
		return out, nil
	})
}

// seeOther is a handler saying the answer is somewhere else: the row a write
// just created, or the list a delete just changed.
type seeOther string

func (s seeOther) Error() string { return "see " + string(s) }

// ok is a page that rendered.
func ok(node g.Node) (*page, error) { return document(node, http.StatusOK) }

// unprocessable is a form re-rendered because the write it carried was refused.
// The status matters twice: the kernel rolls the transaction back on anything
// past 400, and htmx swaps a 422 in place rather than treating it as an error
// nobody sees. See ui/assets/js/htmx-config.js.
func unprocessable(node g.Node) (*page, error) {
	return document(node, http.StatusUnprocessableEntity)
}

// The inputs. huma needs a type per shape, and there are four: a page of a
// list, one row, a form, and a form about one row.
type (
	listInput struct {
		Page int    `query:"page" minimum:"1" default:"1"`
		Sort string `query:"sort"`
	}
	itemInput struct {
		ID uuid.UUID `path:"id" format:"uuid"`
	}
	formInput struct {
		RawBody []byte `contentType:"application/x-www-form-urlencoded"`
	}
	itemFormInput struct {
		ID      uuid.UUID `path:"id" format:"uuid"`
		RawBody []byte    `contentType:"application/x-www-form-urlencoded"`
	}
	emptyInput struct{}
)

// values reads a submitted form into the shape the resource's write takes,
// typed by the schema rather than by guesswork: a number field arrives as a
// number, a checkbox that was not ticked arrives as false rather than as
// missing, and a blank optional field is left out so a nullable column stays
// null instead of becoming the zero time.
func values(body []byte, fields []crud.Field) (map[string]any, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return nil, problem.New(http.StatusUnprocessableEntity, "this form could not be read")
	}
	out := map[string]any{}
	for _, f := range fields {
		if f.ReadOnly {
			continue
		}
		raw, sent := form[f.Name]
		if f.Type == crud.TypeBool {
			// An unticked checkbox sends nothing at all, which is the one case
			// where absence is a value.
			out[f.Name] = sent && raw[0] != "" && raw[0] != "false"
			continue
		}
		if !sent {
			continue
		}
		text := strings.TrimSpace(raw[0])
		if text == "" {
			continue
		}
		switch f.Type {
		case crud.TypeInt:
			n, err := strconv.ParseInt(text, 10, 64)
			if err != nil {
				return nil, invalid(f.Name, "is not a whole number")
			}
			out[f.Name] = n
		case crud.TypeFloat:
			n, err := strconv.ParseFloat(text, 64)
			if err != nil {
				return nil, invalid(f.Name, "is not a number")
			}
			out[f.Name] = n
		case crud.TypeTime:
			// A datetime-local control sends "2026-09-02T14:30" and the API
			// speaks RFC 3339, so the zone the browser did not send is UTC.
			if len(text) == 16 {
				text += ":00"
			}
			if !strings.HasSuffix(text, "Z") && !strings.Contains(text[10:], "+") {
				text += "Z"
			}
			out[f.Name] = text
		case crud.TypeList:
			parts := strings.Split(text, ",")
			list := make([]string, 0, len(parts))
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" {
					list = append(list, p)
				}
			}
			out[f.Name] = list
		default:
			out[f.Name] = text
		}
	}
	return out, nil
}

func invalid(field, why string) error {
	p := problem.New(http.StatusUnprocessableEntity, field+" "+why)
	p.Errors = []string{field + ": " + why}
	return p
}

// fieldErrors reads a problem back into the fields it is about, so a form marks
// the control rather than only shouting above it. kit/problem's Errors carry
// "field: message"; a Detail that names a field is matched too, because
// kit/crud's own messages are prose that names it.
func fieldErrors(err error, fields []crud.Field) (map[string]string, string) {
	p, ok := err.(*problem.Problem)
	if !ok {
		return nil, err.Error()
	}
	out := map[string]string{}
	for _, e := range p.Errors {
		if name, message, found := strings.Cut(e, ": "); found {
			if _, known := crud.FieldNamed(fields, name); known {
				out[name] = message
			}
		}
	}
	detail := strings.TrimPrefix(p.Detail, "crud: invalid: ")
	for _, f := range fields {
		if _, taken := out[f.Name]; !taken && strings.Contains(detail, f.Name) {
			out[f.Name] = detail
		}
	}
	return out, detail
}
