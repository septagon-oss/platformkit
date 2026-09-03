// Package internal is the shell's implementation: the frame, the pages written
// by hand, and the screens generated from every resource kit/rest registered.
package internal

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// Shell is what the manifest hands the implementation.
type Shell struct {
	Nav       []module.NavEntry
	Authorize httpx.Authorizer
	Tenants   tenantcontracts.Service
	Token     tenancy.SystemToken
	// Theme is the installation's two palettes: the one thing about the look of
	// this shell that belongs to whoever runs it. See design.Pair.
	Theme design.Pair
	// served is every path this application answers a GET on, so the sidebar
	// shows a link only when there is something at the end of it; operator is
	// every permission a route declared as the operator's, so a nav entry
	// guarded by one is shown at the operator's tenant and nowhere else. Both
	// are read off the kernel's own recording in Mount, not written by hand.
	served   map[string]bool
	operator map[string]bool
}

// html mounts one page: the operation this shell puts on all of them, and then
// the kernel's own HTML door (httpx.HTML), which records it, hides it from the
// OpenAPI document and turns a SeeOther into the redirect.
//
// What is left here is what belongs to this shell: the tag, the sign-in form an
// anonymous visitor is sent to, and the error page. A handler answers with a
// page or with an error; httpx.SeeOther is the third answer, and it is an error
// because "not this page" is a return a handler already has.
func html[I any](api *httpx.API, s *Shell, id, method, path, summary string, auth httpx.Auth,
	handler func(context.Context, *I) (*httpx.Page, error),
) {
	op := huma.Operation{
		OperationID: id, Method: method, Path: path, Summary: summary,
		Tags: []string{"admin"},
	}
	// Every page here says where its sign-in form is, so a person who follows a
	// bookmark into the shell while signed out is sent to it rather than shown
	// a JSON document about their own anonymity. The API routes beside them say
	// nothing and keep the problem document. See httpx.SignInExtension.
	httpx.SignIn(&op, adminRoot+"/login")
	httpx.HTML(api, op, auth, func(ctx context.Context, in *I) (*httpx.Page, error) {
		out, err := handler(ctx, in)
		var to httpx.SeeOther
		if err != nil && !errors.As(err, &to) {
			return s.fault(ctx, err)
		}
		return out, err
	})
}

// ok is a page that rendered.
func ok(node g.Node) (*httpx.Page, error) { return httpx.Document(node, http.StatusOK) }

// unprocessable is a form re-rendered because the write it carried was refused.
// The status matters twice: the kernel rolls the transaction back on anything
// past 400, and htmx swaps a 422 in place rather than treating it as an error
// nobody sees. See ui/assets/js/htmx-config.js.
func unprocessable(node g.Node) (*httpx.Page, error) {
	return httpx.Document(node, http.StatusUnprocessableEntity)
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
