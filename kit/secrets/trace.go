package secrets

import (
	"context"

	"github.com/google/uuid"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/septagon-oss/platformkit/kit/telemetry"
)

// The attribute keys a secret read carries. They are constants for the reason
// kit/telemetry gives for its own: an attribute spelled twice is one a dashboard
// cannot filter on, and one whose writers nobody can find by grep.
const (
	AttrAdapter = "pkit.secret.adapter"
	AttrRef     = "pkit.secret.ref"
	AttrVersion = "pkit.secret.version"
	AttrBytes   = "pkit.secret.bytes"
)

// Trace returns store with every call spanned under the name adapter — which is
// the whole of this port's trail, and why the span sits here rather than in an
// adapter: a read traced by whichever adapter happened to be selected is a read
// traced only sometimes. Each method opens one span carrying the adapter, the
// address, the outcome, and the one derived fact the call answers with — the
// store's version token for a read, the plaintext's length for an envelope call.
//
// What no span carries is a byte, and no span carries a *tenant's* version token
// either: such a token is a store's own handle on one customer's secret, in the
// class of a session id — worth having in a trace of the installation's own
// secrets, not worth having in one a log reader can use to name a tenant's bytes.
// Sealing and opening are therefore auditable at the key — which key, how many
// bytes it wrapped — and never at the plaintext, which is the most any envelope
// store can offer without holding the plaintext itself.
func Trace(adapter string, store Store) Store { return &traced{adapter: adapter, store: store} }

type traced struct {
	adapter string
	store   Store
}

func (t *traced) Get(ctx context.Context, ref Ref) (secret Secret, err error) {
	ctx, done := start(ctx, "secrets.get", t.adapter, ref)
	defer func() { done(err) }()
	secret, err = t.store.Get(ctx, ref)
	if err == nil && ref.Tenant() == uuid.Nil && secret.Version != "" {
		// Only the installation's own token may be written: see the note above.
		trace.SpanFromContext(ctx).SetAttributes(attribute.String(AttrVersion, secret.Version))
	}
	return secret, err
}

func (t *traced) Version(ctx context.Context, ref Ref) (version string, err error) {
	ctx, done := start(ctx, "secrets.version", t.adapter, ref)
	defer func() { done(err) }()
	version, err = t.store.Version(ctx, ref)
	if err == nil && ref.Tenant() == uuid.Nil && version != "" {
		trace.SpanFromContext(ctx).SetAttributes(attribute.String(AttrVersion, version))
	}
	return version, err
}

func (t *traced) Seal(ctx context.Context, ref Ref, plaintext []byte) (sealed []byte, err error) {
	ctx, done := start(ctx, "secrets.seal", t.adapter, ref, attribute.Int(AttrBytes, len(plaintext)))
	defer func() { done(err) }()
	sealed, err = t.store.Seal(ctx, ref, plaintext)
	return sealed, err
}

func (t *traced) Open(ctx context.Context, ref Ref, sealed []byte) (plain []byte, err error) {
	ctx, done := start(ctx, "secrets.open", t.adapter, ref, attribute.Int(AttrBytes, len(sealed)))
	defer func() { done(err) }()
	plain, err = t.store.Open(ctx, ref, sealed)
	return plain, err
}

// start opens one call's span: the tenant and request id the context holds, the
// adapter, and the address as Ref.String spells it, which is the form that names a
// secret without naming a whole tenant id or any value. The returned func closes
// the span with the outcome, written once on the way out so a new return path
// cannot leave the span saying "ok".
func start(ctx context.Context, op, adapter string, ref Ref, extra ...attribute.KeyValue) (context.Context, func(error)) {
	attrs := append(telemetry.SpanAttrs(ctx), append(extra,
		attribute.String(AttrAdapter, adapter), attribute.String(AttrRef, ref.String()))...)
	ctx, span := telemetry.Tracer().Start(ctx, op, trace.WithAttributes(attrs...))
	return ctx, func(err error) {
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}
