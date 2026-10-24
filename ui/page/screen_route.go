package page

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

func pageOperation(r *httpx.Router, s Shell, rt Route) huma.Operation {
	op := huma.Operation{
		OperationID: rt.ID, Method: rt.Method, Path: rt.Path, Summary: rt.Summary,
		Tags: []string{s.Tag}, Errors: rt.Errors,
	}
	contract := screen.ResponseSet{Exception: rt.Exception, Reason: rt.Reason, Targets: rt.Targets}
	if rt.View != "" {
		kind := rt.Response
		if kind == "" {
			kind = "input"
		}
		contract.Responses = []screen.Response{{View: rt.View, Kind: kind}}
	}
	r.DescribePage(&op, contract)
	if rt.StreamsBody {
		key, streamed := httpx.StreamedBody()
		op.Extensions[key] = streamed
	}
	if rt.MintsCredential {
		if op.Extensions == nil {
			op.Extensions = map[string]any{}
		}
		op.Extensions[httpx.CredentialExtension] = true
	}
	if s.Chrome.SignIn != "" {
		httpx.SignIn(&op, s.Chrome.SignIn)
	}
	return op
}

func attachScreenView(rt Route, err error, v *View) {
	if err == nil && rt.View != "" && (rt.View.Kind() == "custom" || rt.View.Kind() == "command") {
		v.Body = []g.Node{components.Stack(components.StackProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-screen-view": string(rt.View)}}, Gap: "6"}, v.Body...)}
	}
}
