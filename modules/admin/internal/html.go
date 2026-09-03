// Package internal is the shell's implementation: the frame, the pages written
// by hand, and the screens generated from every resource kit/rest registered.
package internal

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
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
	// Theme is the installation's two palettes: the one thing about the look of
	// this shell that belongs to whoever runs it. See design.Pair.
	Theme design.Pair
	// served is every path this application answers a GET on, so the sidebar
	// shows a link only when there is something at the end of it; operatorOnly
	// are the ones only the operator's own tenant may follow.
	served       map[string]bool
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
	op := huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"admin"}, Hidden: true,
		Errors: []int{http.StatusNotFound, http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
	}
	// Every page here says where its sign-in form is, so a person who follows a
	// bookmark into the shell while signed out is sent to it rather than shown
	// a JSON document about their own anonymity. The API routes beside them say
	// nothing and keep the problem document. See httpx.SignInExtension.
	httpx.SignIn(&op, adminRoot+"/login")
	httpx.Register(api, op, auth, func(ctx context.Context, in *I) (*page, error) {
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
