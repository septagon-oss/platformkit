package page

import (
	"cmp"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

// screenOperation is a page route's huma operation with its screen contract
// attached: the views it may answer with and the form addresses its answer may
// target. An empty contract declares nothing beyond the route itself.
func screenOperation(r *httpx.Router, s Shell, rt Route) huma.Operation {
	op := pageOperation(s, rt)
	contract := screen.ResponseSet{Exception: rt.Exception, Reason: rt.Reason, Targets: rt.Targets}
	if rt.View != "" {
		contract.Responses = []screen.Response{{View: rt.View, Kind: cmp.Or(rt.Response, "input")}}
	}
	r.DescribePage(&op, contract)
	return op
}

func attachScreenView(rt Route, err error, v *View) {
	if err == nil && rt.View != "" && (rt.View.Kind() == "custom" || rt.View.Kind() == "command") {
		v.Body = []g.Node{components.Stack(components.StackProps{ComponentProps: components.ComponentProps{Attrs: map[string]string{"data-screen-view": string(rt.View)}}, Gap: "6"}, v.Body...)}
	}
}
