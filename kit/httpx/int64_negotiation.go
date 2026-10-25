package httpx

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
)

func decimalAccepted(accept string) bool {
	legacy, exact := mediaQuality(accept, "application/json"), mediaQuality(accept, int64JSONMediaType)
	return exact.specificity == 2 && exact.quality > 0 && (exact.quality > legacy.quality || (exact.quality == legacy.quality && exact.specificity > legacy.specificity))
}

type acceptQuality struct {
	quality     float64
	specificity int
}

func mediaQuality(accept, target string) acceptQuality {
	best := acceptQuality{specificity: -1}
	if accept == "" && target == "application/json" {
		return acceptQuality{quality: 1}
	}
	for part := range strings.SplitSeq(accept, ",") {
		media, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		specificity := -1
		switch media {
		case target:
			specificity = 2
		case "application/*":
			specificity = 1
		case "*/*":
			specificity = 0
		}
		if specificity < best.specificity || specificity < 0 {
			continue
		}
		quality := 1.0
		if q, ok := params["q"]; ok {
			quality, err = strconv.ParseFloat(q, 64)
			if err != nil || quality < 0 || quality > 1 {
				quality = 0
			}
		}
		if specificity > best.specificity || quality > best.quality {
			best = acceptQuality{quality, specificity}
		}
	}
	return best
}

func mergeVary(header http.Header, name string) {
	for _, value := range header.Values("Vary") {
		for member := range strings.SplitSeq(value, ",") {
			if strings.EqualFold(strings.TrimSpace(member), name) || strings.TrimSpace(member) == "*" {
				return
			}
		}
	}
	header.Add("Vary", name)
}

func (a *API) integerDocumentation(next func(huma.Context)) func(huma.Context) {
	return func(ctx huma.Context) {
		r, _ := humachi.Unwrap(ctx)
		values, present := r.URL.Query()["int64"]
		if !present {
			next(ctx)
			return
		}
		if len(values) != 1 || values[0] != "string" {
			huma.WriteErr(a.api, ctx, http.StatusBadRequest, "int64 must be specified once as string")
			return
		}
		data, err := a.integerDocument()
		if err != nil {
			huma.WriteErr(a.api, ctx, http.StatusInternalServerError, "cannot render the integer contract", err)
			return
		}
		ctx.SetHeader("Content-Type", "application/openapi+json")
		_, _ = ctx.BodyWriter().Write(data)
	}
}

func (a *API) replayRepresentation(ctx huma.Context, contentType string, status int) bool {
	if status < 200 || status >= 300 {
		return true
	}
	media, _, _ := mime.ParseMediaType(contentType)
	if media != "application/json" && media != int64JSONMediaType {
		return true
	}
	selected := "application/json"
	for _, operation := range a.integers {
		if operation.op == ctx.Operation() && operation.eligible && decimalAccepted(ctx.Header("Accept")) {
			selected = int64JSONMediaType
			break
		}
	}
	_, w := humachi.Unwrap(ctx)
	mergeVary(w.Header(), "Accept")
	if media == selected {
		return true
	}
	huma.WriteErr(a.api, ctx, http.StatusConflict, "this command already completed in another representation; request its original representation or read the committed resource")
	return false
}
