package page

// The page a person gets when the kernel refused before any handler ran.
//
// ui/page has always made a document for a handler's own 4xx — Serve turns a returned
// problem into Fault. What it never had was the other half: a cross-site write refused
// by the CSRF guard, a handler that panicked, a request refused by a guard that runs
// ahead of routing. Those never reach a handler, so they answered a browser with a JSON
// body and a request id that could not be selected from a JSON blob in a window frame.
//
// FaultHandler is how the presentation layer closes that. The application registers it
// once at httpx.New, and every guard in the kernel then answers a navigating client with
// this shell's page — same chrome, same stylesheet, same way back, and the verdict's own
// status rather than a 200 that says "Forbidden" in it.

import (
	"net/http"
	"strings"

	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
)

// FaultHandler builds the kernel's failure renderer for one shell. Back and BackLabel
// are the shell's own, so the page offers the way out that exists in that application:
// a generated admin shell sends a stranger to its sign-in; a public site sends them home.
//
// It returns false — leaving the JSON body — for a shell with no frame, because a page
// with no chrome is not a page and a kernel that invented one here would be designing
// interface in the wrong package.
func FaultHandler(s Shell) httpx.Fault {
	return func(w http.ResponseWriter, r *http.Request, p *problem.Problem) bool {
		if s.Frame == nil || p == nil {
			return false
		}
		status := p.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		ctx := r.Context()
		req := read(ctx, s.Chrome)
		v := fault(status, p.Detail, requestID(p.Instance), s.Back, s.BackLabel)

		body := s.Frame(ctx, req, v.Body)
		out, err := Render(Document(s.Chrome, req, v, body), status)
		if err != nil {
			// The document could not be built. Say nothing about why to the person and
			// let the kernel's JSON answer, which is honest about being a failure.
			return false
		}
		w.Header().Set("Content-Type", out.ContentType)
		if out.CacheControl != "" {
			w.Header().Set("Cache-Control", out.CacheControl)
		}
		w.WriteHeader(status)
		_, _ = w.Write(out.Body)
		return true
	}
}

// fault is the refusal document, with the two things a person in front of one actually
// needs: what to do next, and the reference an operator can find the request by in a
// log. The reference is the same URN the JSON body carries, so a screenshot and a log
// line agree — which matters most for the failures that are nobody's fault and still
// happened.
func fault(status int, detail, reference, back, backLabel string) View {
	if strings.TrimSpace(detail) == "" {
		// A 500 carries no detail on purpose: the reason is in the log, not for the
		// browser. The sentence has to be true and useful without it.
		detail = "Something went wrong while handling this."
	}
	if reference != "" {
		// Every verdict carries the reference, and most especially the ones the person
		// can do nothing about: a 403 they may be able to fix themselves, a 500 they can
		// only report. This line used to sit in the else-branch above, which left the 500
		// page — the one page where a reference is the entire value of the visit — with
		// an apology and nothing to quote.
		detail = detail + " (request " + reference + ")"
	}
	return Fault(status, detail, back, backLabel)
}

// requestID is the instance URN read back into the bare identifier, which is what a
// human reads aloud to a human reading a log.
func requestID(instance string) string {
	if instance == "" {
		return ""
	}
	return strings.TrimPrefix(instance, "urn:request:")
}
