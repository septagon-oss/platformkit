package internal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"time"

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
	path       = "/files"
	publicPath = "/files/{id}"
)

// streams is the kernel's mark for a route that reads the request itself, and
// streaming is its value. They are named here because a map literal cannot hold
// a two-value call.
var streams, streaming = httpx.StreamedBody()

var faults = []int{
	http.StatusNotFound, http.StatusRequestEntityTooLarge,
	http.StatusUnprocessableEntity, http.StatusServiceUnavailable,
}

// RegisterRoutes mounts the twelve operations a file has, over seven paths: the
// list, one read, the upload, the content door twice (GET and HEAD), the public
// door twice, the delete, the grant, the hold placed and released, and the
// subject erasure.
//
// There is no rest.Spec, and the reason is one sentence: a Spec's create route
// takes a JSON body, and a file arrives as bytes. The list and the read below
// are the two Spec routes that would have made sense, written out; the create
// is a multipart upload, the update does not exist because a file's bytes are
// what they are, and the delete has an event to publish that the generic one
// could not carry.
func RegisterRoutes(surfaces httpx.Surfaces, svc contracts.Service) {
	app, public := surfaces.App, surfaces.Public
	httpx.Register(app, huma.Operation{
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

	httpx.Register(app, huma.Operation{
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

	httpx.Register(app, huma.Operation{
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
			up.Kind = in.Kind
			up.Image = in.Image
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
		httpx.Register(app, huma.Operation{
			OperationID: "file-file-content" + suffix[method],
			Method:      method,
			Path:        path + "/{id}/content",
			Summary:     "Download a file",
			Description: "The bytes, whatever the file's visibility. Ranges are served. The public door is at " + public.Path(publicPath) + ".",
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
		httpx.Register(public, huma.Operation{
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

	httpx.Register(app, huma.Operation{
		OperationID:   "file-file-delete",
		Method:        http.MethodDelete,
		Path:          path + "/{id}",
		Summary:       "Delete a file",
		Description:   "Removes the record now and the bytes once this commits: a blob delete cannot be rolled back, so it is done by whoever handles file.deleted. A file under a live hold is refused with 409 and nothing is written.",
		Tags:          []string{"file"},
		DefaultStatus: http.StatusNoContent,
		Errors:        append(slices.Clone(faults), http.StatusConflict),
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

	httpx.Register(app, huma.Operation{
		OperationID: "file-file-grant",
		Method:      http.MethodGet,
		Path:        path + "/{id}/grant",
		Summary:     "Mint a time-limited URL for a private file",
		Description: "A URL that serves the bytes until it expires, with no request to this application in the path. A public file is refused: it already has an open door. A store that cannot sign one is refused with 501.",
		Tags:        []string{"file"},
		Errors:      append(slices.Clone(faults), http.StatusNotImplemented),
	}, httpx.Permission(contracts.PermissionFileRead),
		func(ctx context.Context, in *grantInput) (*rest.Item[*contracts.Grant], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			grant, err := svc.Grant(ctx, tx, in.ID, in.Expires)
			if err != nil {
				return nil, fault(err)
			}
			return &rest.Item[*contracts.Grant]{Body: grant}, nil
		})

	// Who reads this file. The ids are read back through the same policy that
	// decided the file was readable at all, so an answer here is scoped to the
	// caller by the database and not by a filter this handler wrote.
	httpx.Register(app, huma.Operation{
		OperationID: "file-file-uses",
		Method:      http.MethodGet,
		Path:        path + "/{id}/uses",
		Summary:     "List the records that reference a file",
		Description: "Which record's field, in which locale, shows this file's image — the records a library's details panel links to. A file nobody shows answers an empty list; a file this caller may not read answers 404.",
		Tags:        []string{"file"},
		Errors:      faults,
	}, httpx.Permission(contracts.PermissionFileRead),
		func(ctx context.Context, in *idInput) (*rest.Item[[]contracts.UseRow], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			uses, err := svc.Uses(ctx, tx, in.ID)
			if err != nil {
				return nil, fault(err)
			}
			return &rest.Item[[]contracts.UseRow]{Body: uses}, nil
		})

	httpx.Register(app, huma.Operation{
		OperationID: "file-file-retain",
		Method:      http.MethodPost,
		Path:        path + "/{id}/hold",
		Summary:     "Place or replace a retention hold",
		Description: "Keeps one file past whatever its retention class says. An until in the past is refused: a hold that has already expired holds nothing.",
		Tags:        []string{"file"},
		Errors:      faults,
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventRetained}},
	}, httpx.Permission(contracts.PermissionFileRetain),
		func(ctx context.Context, in *retainInput) (*rest.Item[*contracts.Hold], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			var until *time.Time
			reason := ""
			if in.Body != nil {
				until, reason = in.Body.Until, in.Body.Reason
			}
			hold, err := svc.Retain(ctx, tx, in.ID, until, reason)
			if err != nil {
				return nil, fault(err)
			}
			return &rest.Item[*contracts.Hold]{Body: hold}, nil
		})

	httpx.Register(app, huma.Operation{
		OperationID:   "file-file-release",
		Method:        http.MethodDelete,
		Path:          path + "/{id}/hold",
		Summary:       "Release a retention hold",
		Description:   "The file goes back to its class's policy. No hold is not an error.",
		Tags:          []string{"file"},
		DefaultStatus: http.StatusNoContent,
		Errors:        faults,
		Extensions:    map[string]any{httpx.EventsExtension: []string{contracts.EventReleased}},
	}, httpx.Permission(contracts.PermissionFileRetain),
		func(ctx context.Context, in *idInput) (*struct{}, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			if err := svc.Release(ctx, tx, in.ID); err != nil {
				return nil, fault(err)
			}
			return nil, nil
		})

	httpx.Register(app, huma.Operation{
		OperationID: "file-file-erase",
		Method:      http.MethodPost,
		Path:        path + "/erase",
		Summary:     "Erase a subject's files",
		Description: "Removes every file this subject uploaded and the bytes beside each one, and leaves one proof row per blob with the digest that is gone and the reason it was asked for. One held file refuses the whole erasure with 409 and names it; a subject with no files answers a receipt of zero and writes nothing.",
		Tags:        []string{"file"},
		Errors:      append(slices.Clone(faults), http.StatusConflict),
		Extensions:  map[string]any{httpx.EventsExtension: []string{contracts.EventDeleted, contracts.EventErased}},
	}, httpx.Permission(contracts.PermissionFileErase),
		func(ctx context.Context, in *eraseInput) (*rest.Item[*contracts.ErasureReceipt], error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, err
			}
			subject, reason := uuid.Nil, ""
			if in.Body != nil {
				subject, reason = in.Body.Subject, in.Body.Reason
			}
			receipt, err := svc.EraseSubject(ctx, tx, subject, reason)
			if err != nil {
				return nil, fault(err)
			}
			return &rest.Item[*contracts.ErasureReceipt]{Body: receipt}, nil
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
	return contracts.ContentResponse(r, f, body, contracts.Inline), nil
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
	switch {
	case errors.Is(err, contracts.ErrTooLarge), errors.Is(err, contracts.ErrQuota):
		return problem.New(http.StatusRequestEntityTooLarge, err.Error())
	// A frame over the pixel ceiling is the same remedy as a file over the byte
	// limit — send something smaller — so it joins the 413 above rather than
	// inventing a status this route has never declared. A file offered as an
	// image that no decoder reads is the request's to fix, which is what 422
	// means; 415 would be truer to the media type and less true to the answer,
	// because the bytes are fine as the document they were not offered as.
	case errors.Is(err, contracts.ErrTooManyPixels), errors.Is(err, contracts.ErrNotImage):
		return problem.New(http.StatusUnprocessableEntity, err.Error())
	// A caller asked for an expiry this module will not sign, or a grant for a
	// file anybody can already read. Both are the request's to fix, which is
	// what 422 means, and rest.Fault has no way to know that a sentinel this
	// module invented is not an outage.
	case errors.Is(err, contracts.ErrInvalidExpiry), errors.Is(err, contracts.ErrPublicFile),
		errors.Is(err, contracts.ErrInvalidKey):
		return problem.New(http.StatusUnprocessableEntity, err.Error())
	// The store this deployment wired cannot sign. Nothing in the request
	// changes that, so it is not a 4xx, and it is not a 500 either: nothing is
	// broken, this capability was simply not wired.
	case errors.Is(err, contracts.ErrNotSignable):
		return problem.New(http.StatusNotImplemented, err.Error())
	// A hold is why this removal did not happen, and it is the caller's to act
	// on — release the hold or stop asking — which makes it a conflict and not
	// an outage. rest.Fault's ErrConflict arm cannot see it: this sentinel is
	// the module's, and nothing about it says the kernel's word for "those two
	// states cannot both be true". The two routes that refuse say 409 in their
	// declarations, and this is where that promise is kept.
	case errors.Is(err, contracts.ErrHeld):
		return problem.Conflict(err.Error())
	default:
		return rest.Fault(err)
	}
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The file's id"`
}

// grantInput is a download link with an expiry. The zero is
// contracts.DefaultGrantExpiry and not a default tag, so that a caller that
// named nothing and a caller that named fifteen minutes are the same request
// rather than one that kit/httpx has to know about.
type grantInput struct {
	ID      uuid.UUID     `path:"id" format:"uuid" doc:"The file's id"`
	Expires time.Duration `query:"expires" default:"0" doc:"How long the URL lives, as a Go duration; at most 24h"`
}

// retainInput is the hold's body beside the file it names. The body is a
// pointer to its own struct and not flat fields on this one, which is how
// kit/rest spells a command body (commandInput) and not a style choice: huma
// reads a body from a nested struct field, and the JSON fields of a struct that
// also carries a path tag are no body at all — a route written that way answers
// every request as though the caller had sent nothing, and says so in a schema
// whose request body has no properties in it.
type retainInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The file to hold"`
	Body *retainBody
}

type retainBody struct {
	// The two bounds are the column's own: file_holds.reason is CHECKed not empty
	// and contracts.MaxHoldReason is the width the entity refuses to go past. They
	// are written here in the schema's literals because a struct tag cannot name a
	// constant — the same reason eraseBody carries its 500 — and they are written
	// at all because a schema that promises no width tells a caller a 4 kB sentence
	// is a request, and the answer they get is a 422 about somebody else's column.
	Until  *time.Time `json:"until,omitempty" format:"date-time" doc:"When the hold expires; omit to hold until released" required:"false"`
	Reason string     `json:"reason" minLength:"1" maxLength:"500" doc:"Why it is held" example:"court order 2026-0412"`
}

type eraseInput struct {
	Body *eraseBody
}

type eraseBody struct {
	Subject uuid.UUID `json:"subject" format:"uuid" doc:"The subject whose files are being erased"`
	Reason  string    `json:"reason" maxLength:"500" doc:"Why, for the audit trail: kept with every removal this erasure makes, in the work order and in the proof row beside the digest"`
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
	// Kind is the retention class the product is filing this under. It is a
	// query parameter for the same reason visibility is: a form field can
	// arrive after the file part, and a class the sweep reads has to be decided
	// before the bytes are stored. An empty kind means no class, and a file with
	// no class is never swept.
	Kind string `query:"kind" default:"" maxLength:"32" doc:"Retention class, as the product names it"`
	// Image is the caller saying this part is an image: the media library and
	// the editor's image field set it, and it is what turns a file no decoder
	// reads into a refusal rather than an attachment. It is a query parameter
	// for the same reason the two above are.
	Image bool `query:"image" default:"false" doc:"Treat these bytes as an image: measure the frame, and refuse what no decoder reads"`
}
