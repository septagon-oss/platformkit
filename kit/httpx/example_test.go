package httpx_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
)

func ExampleRegister() {
	store := cache.Memory("example-httpx")
	defer func() {
		if err := store.Close(); err != nil {
			panic(err)
		}
	}()
	f := &fixture{}
	api, _ := httpx.New(httpx.Options{
		Docs: true, Unwired: true, Cache: store,
		Tenants: f, Authorize: f, Authenticate: f.authenticate,
	})
	type input struct {
		Body struct{ Text string }
	}
	type output struct {
		Body struct{ Length int }
	}
	httpx.Register(api.Surfaces("examples").App, huma.Operation{
		OperationID: "examples-check", Method: http.MethodPost, Path: "/checks",
	}, httpx.Permission("example:check"), func(_ context.Context, in *input) (*output, error) {
		out := new(output)
		out.Body.Length = len(in.Body.Text)
		return out, nil
	})
	if err := api.ValidateDeclarations(); err != nil {
		panic(err)
	}
	// Unwired permits metadata inspection without serving a tenant request.
	for _, op := range api.Recorded() {
		if op.OperationID == "examples-check" {
			fmt.Println(op.Method, op.Path, op.OperationID)
		}
	}
	// Output: POST /api/v1/examples/checks examples-check
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExampleRegister")
}
