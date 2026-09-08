package contracts

import (
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
)

// Disposition selects download or inline presentation. Inline still enforces
// Renderable; no caller may turn an active document into same-origin markup.
type Disposition string

const (
	Attachment Disposition = "attachment"
	Inline     Disposition = "inline"
)

// The three headers a download carries besides its type, and the reason each
// one is there.
//
// The policy allows nothing at all and sandboxes the document, so an uploaded
// page that reaches a browser anyway — through a proxy that rewrote the
// disposition, through a caller that saved and opened it — has no origin, no
// scripts and no forms. sandbox with no value is the strictest form there is.
//
// same-site refuses embedding from unrelated sites; authorization remains the
// caller's responsibility, including requests from another same-site origin.
const (
	downloadPolicy = "default-src 'none'; sandbox"
	downloadCORP   = "same-site"
	// What a private file's response says about caching. no-store rather than
	// private, because "private" still lets the browser keep it on disk for
	// whoever uses the machine next.
	downloadPrivate = "no-store"
)

// ContentResponse serves an already authorized, opened file and closes its body.
// The caller owns tenant and business access checks on every request, including
// HEAD and ranges. Seekable storage supports ranges; other storage sends the full
// body without buffering it into memory. The request is supplied explicitly.
func ContentResponse(r *http.Request, f *File, body io.ReadCloser, mode Disposition) *huma.StreamResponse {
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer body.Close()
		hctx.SetHeader("Content-Type", f.ContentType)
		// The stored type is what the upload declared, so a browser must not be
		// allowed to decide it is something more interesting.
		hctx.SetHeader("X-Content-Type-Options", "nosniff")
		hctx.SetHeader("Content-Security-Policy", downloadPolicy)
		hctx.SetHeader("Cross-Origin-Resource-Policy", downloadCORP)
		hctx.SetHeader("Content-Disposition", disposition(f, mode))
		if !f.Public() {
			// A private file is one tenant's own, and a browser or a proxy
			// caches what nothing told it not to: the next person to ask that
			// cache for this URL must not be handed the bytes. A public file is
			// deliberately cacheable and says nothing here.
			hctx.SetHeader("Cache-Control", downloadPrivate)
		}

		// A seekable body is a range request, a HEAD and a conditional get, all
		// of which net/http already implements correctly and none of which are
		// worth a second implementation here. The local storage returns an
		// *os.File; an implementation that streams from somewhere else does not,
		// and gets the whole file as before.
		seeker, seekable := body.(io.ReadSeeker)
		w, writable := hctx.BodyWriter().(http.ResponseWriter)
		if seekable && writable && r != nil {
			// The status has to reach huma as well as the wire: kit/httpx
			// decides whether the request's transaction commits from what the
			// handler said the status was, and a handler that wrote its own
			// response and told huma nothing is one that rolls back. So the
			// status ServeContent chose — 200, 206 for a range, 304 for a
			// conditional get — is captured and handed over afterwards, when
			// the header it would write is already on the buffer and the second
			// WriteHeader is ignored.
			rec := &recorder{ResponseWriter: w, private: !f.Public()}
			http.ServeContent(rec, r, f.Name, f.UpdatedAt, seeker)
			hctx.SetStatus(rec.status)
			return
		}
		hctx.SetHeader("Content-Length", strconv.FormatInt(f.Size, 10))
		hctx.SetStatus(http.StatusOK)
		if r == nil || r.Method != http.MethodHead {
			_, _ = io.Copy(hctx.BodyWriter(), body)
		}
	}}
}

// recorder is the status http.ServeContent decided. See its one caller.
type recorder struct {
	http.ResponseWriter
	status  int
	private bool
}

func (rec *recorder) WriteHeader(status int) {
	// ServeContent removes cache headers on errors such as an invalid range.
	// Even those responses must not enter a shared cache for a private URL.
	if rec.private {
		rec.Header().Set("Cache-Control", downloadPrivate)
	}
	if rec.status == 0 {
		rec.status = status
	}
	rec.ResponseWriter.WriteHeader(status)
}

func (rec *recorder) Write(p []byte) (int, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	return rec.ResponseWriter.Write(p)
}

func (rec *recorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// disposition is attachment unless the stored type is one this application will
// render. The name is always carried, because a browser that saves a file with
// a uuid for a name is a browser nobody can use.
func disposition(f *File, mode Disposition) string {
	kind := "attachment"
	if mode == Inline && Renderable(f.ContentType) {
		kind = "inline"
	}
	// FormatMediaType returns "" for a name it cannot encode, which is a name
	// with a control character or an unpaired surrogate in it. The type on its
	// own is still the decision that matters.
	if out := mime.FormatMediaType(kind, map[string]string{"filename": f.Name}); out != "" {
		return out
	}
	return kind
}
