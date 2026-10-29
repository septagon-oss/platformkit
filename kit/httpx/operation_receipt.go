package httpx

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/idempotency"
	"github.com/septagon-oss/platformkit/kit/request"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

const (
	OperationIDHeader       = "Operation-ID"
	OperationLocationHeader = "Operation-Location"
)

func (a *API) operationAdmission(ctx huma.Context, next func(huma.Context)) {
	if !unsafeMethod(ctx.Method()) {
		next(ctx)
		return
	}
	t, _ := tenancy.FromContext(ctx.Context())
	if t.ID == uuid.Nil {
		a.refuse(ctx, http.StatusBadRequest, "a write needs a resolved tenant")
		return
	}
	raw := ctx.Header(IdempotencyKeyHeader)
	if raw != "" && !validKey(raw) {
		a.refuse(ctx, http.StatusBadRequest, CodeIdempotencyKey+": "+keyRule)
		return
	}
	key := strings.Trim(raw, `"`)
	if key == "" {
		key = uuid.NewString()
	}
	if a.opts.Idempotency == nil {
		a.refuse(ctx, http.StatusServiceUnavailable, CodeIdempotencyUnavailable)
		return
	}
	r, _ := humachi.Unwrap(ctx)
	body, ok := a.operationBody(ctx, r, raw)
	if !ok {
		return
	}
	p, _ := tenancy.PrincipalFrom(ctx.Context())
	caller := p.UserID
	if auth, _ := declarationOf(ctx.Operation()); auth.kind == kindBearerKey {
		caller = p.TokenID
	}
	scope := idempotency.Scope{Tenant: t.ID, Caller: caller, Method: ctx.Method(), Route: routeOf(ctx), Key: key, Track: true,
		Fingerprint: fingerprint(ctx.Method(), routeOf(ctx), addressOf(r), r.URL.RawQuery, r.Header.Get("Content-Type"), body)}
	at := a.opts.Clock().UTC().Truncate(time.Microsecond)
	v, err := a.opts.Idempotency.Claim(ctx.Context(), scope, at)
	if err != nil {
		a.operationUnavailable(ctx, err)
		return
	}
	operationHeaders(ctx, key, v.OperationRef, p.UserID)
	switch v.Kind {
	case idempotency.First:
		a.beginOperation(ctx, next, scope, v, p.UserID, at)
	case idempotency.Replay:
		a.replay(ctx, v.Stored)
	case idempotency.ConflictBody:
		a.refuse(ctx, http.StatusUnprocessableEntity, CodeIdempotencyConflict)
	case idempotency.InFlight:
		a.refuse(ctx, http.StatusConflict, CodeIdempotencyInFlight)
	case idempotency.Spent:
		a.refuse(ctx, http.StatusConflict, CodeIdempotencySpent)
	default:
		a.refuse(ctx, http.StatusInternalServerError, "")
	}
}

func (a *API) operationUnavailable(ctx huma.Context, err error) {
	a.rlog(ctx.Context()).ErrorContext(ctx.Context(), "httpx: operation storage refused admission", "error", err)
	a.refuse(ctx, http.StatusServiceUnavailable, CodeIdempotencyUnavailable)
}

func operationNote(ctx context.Context) (*claimNote, bool) {
	n, ok := ctx.Value(claimKey{}).(*claimNote)
	return n, ok && n.operation
}

func (a *API) finishOperation(n *claimNote) error {
	if request.OperationDeferred(n.ctx) {
		return nil
	}
	tx, ok := TxFrom(n.ctx)
	if !ok {
		return db.ErrNoTenant
	}
	op, _ := request.OperationFrom(n.ctx)
	r, err := a.opts.Operations.Current(n.ctx, tx, op)
	if err != nil {
		return err
	}
	if r.Outcome != "" {
		return nil
	}
	return events.Succeed(n.ctx, tx, a.opts.Clock())
}

func (a *API) failedOperation(n *claimNote) error {
	c, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(n.ctx)), 5*time.Second)
	defer cancel()
	return db.Run(c, a.opts.Conn, func(c context.Context, tx db.Tx[db.Tenant]) error {
		if err := n.store.Mark(c, tx, n.scope, n.claim); err != nil {
			return err
		}
		op, _ := request.OperationFrom(c)
		r, err := a.opts.Operations.Current(c, tx, op)
		if err != nil {
			return err
		}
		if r.Outcome == request.Succeeded || r.Outcome == request.Failed {
			return nil
		}
		return events.RecordOutcome(c, tx, r.Revision, request.Failed, a.opts.Clock())
	})
}

func (a *API) operationBody(ctx huma.Context, r *http.Request, raw string) ([]byte, bool) {
	ceiling := int64(maxKeyedBody)
	if raw == "" {
		ceiling = max(ceiling, a.opts.MaxUpload)
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, ceiling+1))
	if err != nil {
		if timeout, ok := errors.AsType[net.Error](err); ok && timeout.Timeout() {
			a.refuse(ctx, http.StatusInternalServerError, "")
			return nil, false
		}
		a.refuse(ctx, http.StatusBadRequest, "the write body could not be read")
		return nil, false
	}
	if int64(len(body)) > ceiling {
		a.refuse(ctx, http.StatusRequestEntityTooLarge, CodeIdempotencyTooLarge)
		return nil, false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if carried, ok := RequestFrom(ctx.Context()); ok && carried != nil && carried != r {
		carried.Body = io.NopCloser(bytes.NewReader(body))
	}
	return body, true
}

func operationHeaders(ctx huma.Context, key string, ref, person uuid.UUID) {
	if ref != uuid.Nil {
		ctx.SetHeader(OperationIDHeader, key)
		if b, ok := bufferFrom(ctx.Context()); ok {
			b.header.Set(OperationIDHeader, key)
		}
		if person != uuid.Nil {
			location := "/api/v1/app/operations/me/" + ref.String()
			ctx.SetHeader(OperationLocationHeader, location)
			if b, ok := bufferFrom(ctx.Context()); ok {
				b.header.Set(OperationLocationHeader, location)
			}
		}
	}
}

func (a *API) beginOperation(ctx huma.Context, next func(huma.Context), scope idempotency.Scope, v idempotency.Verdict, person uuid.UUID, at time.Time) {
	if v.OperationRef == uuid.Nil {
		a.refuse(ctx, http.StatusConflict, CodeIdempotencySpent)
		return
	}
	receipt := idempotency.Receipt{Ref: v.OperationRef, ID: scope.Key, Tenant: scope.Tenant, Caller: scope.Caller, Person: person, Method: scope.Method, Route: scope.Route, Revision: 1, StartedAt: v.StartedAt}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx.Context())), 5*time.Second)
	err := db.Run(detached, a.opts.Conn, func(c context.Context, tx db.Tx[db.Tenant]) error { return a.opts.Operations.Reserve(c, tx, receipt) })
	cancel()
	if err != nil {
		a.operationUnavailable(ctx, err)
		return
	}
	if err = a.opts.Idempotency.Begin(ctx.Context(), scope, v.Claim, at); err != nil {
		a.operationUnavailable(ctx, err)
		return
	}
	ctx = huma.WithContext(ctx, request.WithOperationClock(ctx.Context(), receipt.Operation(), a.opts.Clock))
	noteClaim(ctx, a.opts.Idempotency, scope, v.Claim)
	if n, ok := ctx.Context().Value(claimKey{}).(*claimNote); ok {
		n.operation = true
		n.noReplay = mintsACredential(ctx.Operation()) || !keyedMethod(ctx.Method()) || !keyedSurface(ctx.Context())
	}
	if b, ok := bufferFrom(ctx.Context()); ok {
		b.mutation = true
	}
	next(ctx)
}
