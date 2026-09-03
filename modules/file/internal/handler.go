package internal

import (
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// The two paths: the collection an administrator works with, and the one route
// a visitor reaches. They are siblings rather than one path with two
// authorizations, because a route is guarded by what it is and not by who asks.
const (
	path       = "/api/v1/file/files"
	publicPath = "/api/v1/file/public/{id}"
)

// streams is the kernel's mark for a route that reads the request itself, and
// streaming is its value. They are named here because a map literal cannot hold
// a two-value call.
var streams, streaming = httpx.StreamedBody()

var faults = []int{
	http.StatusNotFound, http.StatusRequestEntityTooLarge,
	http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
}

// The three headers a download carries besides its type, and the reason each
// one is there.
//
// The policy allows nothing at all and sandboxes the document, so an uploaded
// page that reaches a browser anyway — through a proxy that rewrote the
// disposition, through a caller that saved and opened it — has no origin, no
// scripts and no forms. sandbox with no value is the strictest form there is.
//
// same-site is what stops another origin embedding this tenant's private file
// as an image and reading whether it loaded.
const (
	downloadPolicy = "default-src 'none'; sandbox"
	downloadCORP   = "same-site"
)

// RegisterRoutes mounts the six routes a file has.
//
// There is no rest.Spec, and the reason is one sentence: a Spec's create route
// takes a JSON body, and a file arrives as bytes. The list and the read below
// are the two Spec routes that would have made sense, written out; the create
// is a multipart upload, the update does not exist because a file's bytes are
// what they are, and the delete has an event to publish that the generic one
// could not carry.
func RegisterRoutes(api *httpx.API, svc contracts.Service) {
	httpx.Register(api, huma.Operation{
		OperationID: "file-file-list",
		Method:      http.MethodGet,
		Path:        path,
		Summary:     "List files",
		Description: "The tenant's files, newest first. The bytes are at /{id}/content.",
		Tags:        []string{"file"},
		Errors:      faults,
	}, httpx.Permission(contracts.PermissionFileRead),
		func(ctx context.Context, in *listInput) (*rest.Page[*contracts.File], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			items, total, err := crud.List[*contracts.File](tx, crud.Query{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort})
			if err != nil {
				return nil, fault(err)
			}
			out := &rest.Page[*contracts.File]{}
			out.Body.Items, out.Body.Total, out.Body.Limit, out.Body.Offset = items, total, in.Limit, in.Offset
			return out, nil
		})

	httpx.Register(api, huma.Operation{
		OperationID: "file-file-read",
		Method:      http.MethodGet,
		Path:        path + "/{id}",
		Summary:     "Read a file's record",
		Description: "What is known about the file. The bytes are at /{id}/content.",
		Tags:        []string{"file"},
		Errors:      faults,
	}, httpx.Permission(contracts.PermissionFileRead),
		func(ctx context.Context, in *idInput) (*rest.Item[*contracts.File], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			f, err := crud.Get[*contracts.File](tx, in.ID)
			if err != nil {
				return nil, fault(err)
			}
			return &rest.Item[*contracts.File]{Body: f}, nil
		})

	httpx.Register(api, huma.Operation{
		OperationID:   "file-file-upload",
		Method:        http.MethodPost,
		Path:          path,
		Summary:       "Upload a file",
		Description:   "A multipart form with one file part. The bytes are streamed to storage as they arrive, hashed and counted on the way past; an upload larger than this deployment accepts is refused with 413 and nothing is kept.",
		Tags:          []string{"file"},
		DefaultStatus: http.StatusCreated,
		Errors:        faults,
		// The second extension is what tells the kernel this route reads its
		// own request: no schema means no ceiling of huma's, so the kernel
		// gives it files.max_bytes and a multipart envelope instead of the
		// megabyte every other route gets. See httpx.StreamedBody.
		Extensions: map[string]any{
			httpx.EventsExtension: []string{contracts.EventUploaded},
			streams:               streaming,
		},
		// Declared by hand, with no schema, which is what keeps huma from
		// reading the body: a schema here would make it decode the whole form
		// into memory before this handler ran, and the point of the handler is
		// that nothing is ever held.
		RequestBody: &huma.RequestBody{
			Required: true,
			Content:  map[string]*huma.MediaType{"multipart/form-data": {}},
		},
	}, httpx.Permission(contracts.PermissionFileManage),
		func(ctx context.Context, in *uploadInput) (*rest.Item[*contracts.File], error) {
			up, err := arriving(ctx)
			if err != nil {
				return nil, err
			}
			up.Visibility = in.Visibility
			// transaction and not transaction(ctx): the per-request transaction
			// is lazy, and this route is the one that must not open it before
			// the body has arrived. See contracts.Tx.
			f, err := svc.Upload(ctx, transaction, up)
			if err != nil {
				return nil, fault(err)
			}
			return &rest.Item[*contracts.File]{Body: f}, nil
		})

	// GET and HEAD, twice, because a browser and a downloader both ask for the
	// size before they ask for the bytes and chi mounts no method a route did
	// not declare. The handler is the same one: http.ServeContent writes the
	// headers and no body for a HEAD, which is the whole difference.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		httpx.Register(api, huma.Operation{
			OperationID: "file-file-content" + suffix[method],
			Method:      method,
			Path:        path + "/{id}/content",
			Summary:     "Download a file",
			Description: "The bytes, whatever the file's visibility. Ranges are served. The public door is at " + publicPath + ".",
			Tags:        []string{"file"},
			Errors:      faults,
		}, httpx.Permission(contracts.PermissionFileRead),
			func(ctx context.Context, in *idInput) (*huma.StreamResponse, error) {
				return download(ctx, svc, in.ID, false)
			})
	}

	// The public door. It is Public because that is what a public file is, and
	// it is safe to be because the tenant still comes from the request's own
	// host, the query still runs under that tenant's policy, and a file that is
	// not public is not found rather than forbidden.
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		httpx.Register(api, huma.Operation{
			OperationID: "file-file-public" + suffix[method],
			Method:      method,
			Path:        publicPath,
			Summary:     "Download a public file",
			Description: "The bytes of a file whose visibility is public. Anything else is a 404, so an anonymous caller learns nothing about what this tenant has.",
			Tags:        []string{"file"},
			Errors:      []int{http.StatusNotFound, http.StatusServiceUnavailable},
		}, httpx.Public(), func(ctx context.Context, in *idInput) (*huma.StreamResponse, error) {
			if _, ok := tenancy.FromContext(ctx); !ok {
				return nil, problem.NotFound("no site is served at this host")
			}
			return download(ctx, svc, in.ID, true)
		})
	}

	httpx.Register(api, huma.Operation{
		OperationID:   "file-file-delete",
		Method:        http.MethodDelete,
		Path:          path + "/{id}",
		Summary:       "Delete a file",
		Description:   "Removes the record now and the bytes once this commits: a blob delete cannot be rolled back, so it is done by whoever handles file.deleted.",
		Tags:          []string{"file"},
		DefaultStatus: http.StatusNoContent,
		Errors:        faults,
		Extensions:    map[string]any{httpx.EventsExtension: []string{contracts.EventDeleted}},
	}, httpx.Permission(contracts.PermissionFileManage),
		func(ctx context.Context, in *idInput) (*struct{}, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := svc.Delete(ctx, tx, in.ID); err != nil {
				return nil, fault(err)
			}
			return nil, nil
		})
}

// arriving is the file part of a multipart request, as a reader nothing has
// consumed yet.
//
// It reads the request straight off kit/httpx rather than through huma's own
// multipart decoding, and that is the whole reason the operation above declares
// its body by hand: huma's decoder parses the form before the handler runs,
// which spools every upload past a few kilobytes to a temporary file. Streaming
// means the bytes go to storage once, and an upload past the limit is refused
// having written only as much as the limit.
func arriving(ctx context.Context) (contracts.Upload, error) {
	r, ok := httpx.RequestFrom(ctx)
	if !ok {
		return contracts.Upload{}, problem.New(http.StatusServiceUnavailable, "this request cannot be read")
	}
	parts, err := r.MultipartReader()
	if err != nil {
		return contracts.Upload{}, problem.New(http.StatusUnprocessableEntity,
			"an upload is a multipart form with one file part: "+err.Error())
	}
	for {
		part, err := parts.NextPart()
		if errors.Is(err, io.EOF) {
			return contracts.Upload{}, problem.New(http.StatusUnprocessableEntity, "this form carries no file")
		}
		if err != nil {
			return contracts.Upload{}, problem.New(http.StatusUnprocessableEntity, "this form cannot be read: "+err.Error())
		}
		if part.FileName() == "" {
			// A field rather than a file. Everything this route needs besides
			// the bytes is a query parameter, so there is nothing to read here.
			_ = part.Close()
			continue
		}
		// Declared is -1 and not the request's Content-Length: that is the
		// whole form, boundaries included, and a part declares no length of
		// its own. It is the honest answer, and it is why Storage.Put has to
		// accept one — an implementation that needs a length up front cannot
		// get it from a stream, and pretending otherwise would hand an object
		// store a number that is wrong by the size of a MIME header.
		return contracts.Upload{
			Name: part.FileName(), ContentType: part.Header.Get("Content-Type"),
			Declared: -1, Body: part,
		}, nil
	}
}

// suffix names the HEAD operations apart from the GET ones. An operation id has
// to be unique and it is what a generated client calls the method.
var suffix = map[string]string{http.MethodGet: "", http.MethodHead: "-head"}

// download answers with the bytes. The response is a stream rather than a body
// huma marshals, so a large file is not copied through a buffer on its way out;
// kit/httpx holds a response until the transaction commits and gives up on that
// past two megabytes, which is where a download stops being something worth
// holding.
//
// The disposition is the security decision, and it is a closed allow-list: a
// stored type this application will render is served inline, and everything
// else is an attachment. The set is contracts.Renderable, and the reason it is
// written as what is safe rather than as what is not is that the unsafe set is
// open — text/html, image/svg+xml, application/xhtml+xml, every XML dialect a
// browser will run a script inside, and whatever the next browser adds. An
// uploaded page served inline is stored cross-site scripting on the tenant's
// own origin, with the tenant's own cookies, which is what the review found.
func download(ctx context.Context, svc contracts.Opener, id uuid.UUID, anonymous bool) (*huma.StreamResponse, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, err
	}
	f, body, err := svc.Open(ctx, tx, id, anonymous)
	if err != nil {
		return nil, fault(err)
	}
	r, _ := httpx.RequestFrom(ctx)
	return &huma.StreamResponse{Body: func(hctx huma.Context) {
		defer body.Close()
		hctx.SetHeader("Content-Type", f.ContentType)
		// The stored type is what the upload declared, so a browser must not be
		// allowed to decide it is something more interesting.
		hctx.SetHeader("X-Content-Type-Options", "nosniff")
		hctx.SetHeader("Content-Security-Policy", downloadPolicy)
		hctx.SetHeader("Cross-Origin-Resource-Policy", downloadCORP)
		hctx.SetHeader("Content-Disposition", disposition(f))

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
			rec := &recorder{ResponseWriter: w}
			http.ServeContent(rec, r, f.Name, f.UpdatedAt, seeker)
			hctx.SetStatus(rec.status)
			return
		}
		hctx.SetHeader("Content-Length", strconv.FormatInt(f.Size, 10))
		hctx.SetStatus(http.StatusOK)
		_, _ = io.Copy(hctx.BodyWriter(), body)
	}}, nil
}

// recorder is the status http.ServeContent decided. See its one caller.
type recorder struct {
	http.ResponseWriter
	status int
}

func (rec *recorder) WriteHeader(status int) {
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
func disposition(f *contracts.File) string {
	kind := "attachment"
	if contracts.Renderable(f.ContentType) {
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

// transaction is the request's, or a 503 saying why there is none.
func transaction(ctx context.Context) (db.Tx[db.Tenant], error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return tx, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
	}
	return tx, nil
}

// fault is kit/rest's mapping plus the one status this module has that nothing
// else does: an upload past the limit is 413, which is the only answer a caller
// can act on by sending something smaller.
func fault(err error) error {
	if errors.Is(err, contracts.ErrTooLarge) || errors.Is(err, contracts.ErrQuota) {
		return problem.New(http.StatusRequestEntityTooLarge, err.Error())
	}
	return rest.Fault(err)
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The file's id"`
}

type listInput struct {
	Limit  int    `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset int    `query:"offset" minimum:"0" doc:"Rows to skip"`
	Sort   string `query:"sort" doc:"A field name, or a field name prefixed with - for descending"`
}

// uploadInput is everything about an upload that is not the bytes. Visibility
// is a query parameter and not a form field, because a form field can arrive
// after the file and this handler streams the file the moment it reaches it: a
// decision that arrived too late to be applied is worse than one that has to be
// made in the URL.
type uploadInput struct {
	Visibility string `query:"visibility" enum:"private,public" default:"private" doc:"Who may read the file once it is stored"`
}
