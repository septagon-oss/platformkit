package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
)

type delivery struct {
	source    contracts.Source
	templates map[[32]byte]contracts.Template
	mu        sync.Mutex
	pool      *cache.Pool[string]
}
type pictureInput struct {
	Reference string `path:"reference"`
}

// Register mounts a private image door with an independent empty pool holder.
// No source, cache or connection is consulted until an admitted request arrives.
func Register(router *httpx.Router, source contracts.Source, templates map[[32]byte]contracts.Template) {
	d := &delivery{source: source, templates: templates}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		httpx.Register(router, huma.Operation{OperationID: "picture-content-" + strings.ToLower(method), Method: method, Path: "/pictures/{reference}/content", Summary: "Read a composed picture", Tags: []string{"picture"}}, httpx.SignedIn(), d.serve)
	}
}
func (d *delivery) serve(ctx context.Context, in *pictureInput) (*huma.StreamResponse, error) {
	tx, ok := httpx.TxFrom(ctx)
	if !ok {
		return nil, problem.New(503, "the database is not reachable right now")
	}
	request, ok := httpx.RequestFrom(ctx)
	if !ok {
		return nil, problem.New(500, "")
	}
	if request.URL.RawQuery != "" || request.URL.ForceQuery {
		return nil, problem.New(422, "use the picture address without query parameters")
	}
	key, err := entity.PictureKey(entity.Picture(in.Reference))
	if err != nil {
		return nil, problem.New(422, err.Error())
	}
	input, err := d.source.Load(ctx, tx, key)
	if err != nil {
		return nil, sourceFault(err)
	}
	if input.TemplateDigest == ([32]byte{}) {
		return nil, problem.New(500, "")
	}
	template, ok := d.templates[input.TemplateDigest]
	if !ok {
		return nil, problem.New(410, "the original picture template is no longer declared")
	}
	resolved, err := contracts.Resolve(template, input.Value)
	if err != nil {
		return nil, problem.New(500, "")
	}
	store, ok := httpx.CacheFrom(ctx)
	if !ok {
		return nil, problem.New(503, "the picture cache is not reachable right now")
	}
	d.mu.Lock()
	if d.pool == nil {
		d.pool, err = cache.PoolOf[string](store, "picture", 5*time.Minute, 32)
	}
	pool := d.pool
	d.mu.Unlock()
	if err != nil {
		return nil, problem.New(500, "")
	}
	identity := resolved.Identity()
	scoped := cache.Of(db.TenantOf(tx).ID, "picture").Entry(hex.EncodeToString(identity[:]))
	body, err := pool.Load(ctx, scoped, func(ctx context.Context) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		body, err := contracts.Compose(template, resolved)
		if err != nil {
			return "", err
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return body, nil
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, problem.New(503, "the picture request may be retried")
		}
		return nil, problem.New(500, "")
	}
	return representation(request, body), nil
}
func sourceFault(err error) error {
	switch {
	case errors.Is(err, contracts.ErrUnavailable):
		return problem.New(404, "the picture is not available")
	case errors.Is(err, tenancy.ErrPolicyDenied):
		return problem.New(403, "access to this picture was refused")
	case errors.Is(err, tenancy.ErrPolicyUnavailable), errors.Is(err, contracts.ErrTemporary):
		return problem.New(503, "the picture source is not reachable right now")
	default:
		return problem.New(500, "")
	}
}
func representation(request *http.Request, body string) *huma.StreamResponse {
	digest := sha256.Sum256([]byte(body))
	return &huma.StreamResponse{Body: func(ctx huma.Context) {
		ctx.SetHeader("Content-Type", "image/svg+xml; charset=utf-8")
		ctx.SetHeader("Content-Disposition", "inline")
		ctx.SetHeader("Cache-Control", "private, no-store")
		ctx.SetHeader("X-Content-Type-Options", "nosniff")
		ctx.SetHeader("Content-Security-Policy", "default-src 'none'; sandbox")
		ctx.SetHeader("Cross-Origin-Resource-Policy", "same-site")
		ctx.SetHeader("ETag", fmt.Sprintf(`"%x"`, digest))
		writer, ok := ctx.BodyWriter().(http.ResponseWriter)
		if !ok {
			ctx.SetStatus(500)
			return
		}
		rec := &statusWriter{ResponseWriter: writer}
		http.ServeContent(rec, request, "picture.svg", time.Time{}, strings.NewReader(body))
		ctx.SetStatus(rec.status)
	}}
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "private, no-store")
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(p)
}
func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
