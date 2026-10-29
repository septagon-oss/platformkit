package db_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
)

// TestACrossTenantTransactionNamesTheModuleThatOpenedIt is the reason the
// declaration exists: a module that crosses tenants said why in its manifest, and
// the open has to carry both names. A trail that says only "a module's
// control-plane routes" cannot be read after the fact — it cannot tell the file
// sweep from a tenant directory, which is the difference between an audit and a
// guess.
func TestACrossTenantTransactionNamesTheModuleThatOpenedIt(t *testing.T) {
	_, conn := dbtest.Schema(t)
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	const reason = "sweep stored bytes no row references, across every tenant"
	// Minted through the real door and not dbtest's stand-in: the thing under test
	// is what the door writes into the trail.
	if err := db.RunSystem(t.Context(), conn, syscap.NewSystemTokenFor("file", reason), func(context.Context, db.Tx[db.System]) error {
		return nil
	}); err != nil {
		t.Fatalf("open the cross-tenant transaction: %v", err)
	}

	var spans []string
	for _, span := range recorder.Ended() {
		if span.Name() != "cross-tenant transaction" {
			continue
		}
		for _, attr := range span.Attributes() {
			spans = append(spans, string(attr.Key)+"="+attr.Value.Emit())
		}
	}
	for _, want := range []string{"pkit.system.module=file", "pkit.system.reason=" + reason} {
		if !strings.Contains(strings.Join(spans, " "), want) {
			t.Errorf("the span does not carry %q; it carries %v", want, spans)
		}
	}
	line := logged.String()
	// The text handler quotes a value containing a space, so the reason is asked
	// for as the sentence and not as `reason=<sentence>`.
	for _, want := range []string{"module=file", `reason="` + reason + `"`} {
		if !strings.Contains(line, want) {
			t.Errorf("the log does not carry %q; it says:\n%s", want, line)
		}
	}
}

// TestAKernelsOwnCrossTenantTransactionNamesNoModule keeps the empty case
// truthful: host resolution, the relay and a probe are the kernel crossing
// tenants, and their trail says the kernel rather than borrowing a module's name.
func TestAKernelsOwnCrossTenantTransactionNamesNoModule(t *testing.T) {
	_, conn := dbtest.Schema(t)
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	if err := db.RunSystem(t.Context(), conn, dbtest.SystemToken(), func(context.Context, db.Tx[db.System]) error {
		return nil
	}); err != nil {
		t.Fatalf("open the cross-tenant transaction: %v", err)
	}
	if strings.Contains(logged.String(), "module=file") {
		t.Errorf("a token minted by the kernel named a module in the trail:\n%s", logged.String())
	}
}
