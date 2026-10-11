package health_test

// Review round 3 (T-0110). A new type whose whole job is to not do something, and
// nothing tested that it does not do it.
//
// Report was extracted from Check by this branch (kit/health/health.go:61) for one
// reason, which its own comment and the CHANGELOG both state: "a replica that is
// serving its tenants perfectly must not be pulled out of the rotation because the
// tracing backend is down", and — of the exporter's last success — "an unreachable
// collector fails no boot, moves no health verdict and does not decide an exit
// code". That is an availability claim about a production routing decision: if a
// Report ever moved the verdict, every replica of every deployment would stop taking
// traffic the moment its collector did, and tracing would become the reason an
// application stopped serving — the exact failure the type exists to make impossible.
//
// kit/health/health_test.go was changed by this branch in one respect only: the
// `Register(api, checks ...Check)` call sites became `Register(api, checks)`. Grep
// for Report across this package's tests answers nothing, and the delivery's own
// round-6 report does not name a case for it either. So the claim is carried today by
// the shape of Mux and by nothing else.
//
// The case below pins it: with a Report that reports the collector down and every
// Check passing, /ready answers 200 and *says* the pipeline is down; the same
// handler with a failing Check still answers 503, so the 200 above is a verdict and
// not a probe that stopped working; and the reading that reaches the body carries no
// collector address, which is the redaction kit/app's errExportDown promises. Each
// assertion reads the status and the bytes the handler answered, so it cannot be
// reached through anything a broken version prints.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/health"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// downPipeline is the exporter's reading as kit/app writes it: a worrying message, an
// error, and — by design — no address in it.
type downPipeline struct{}

func (downPipeline) Name() string { return "telemetry" }

func (downPipeline) Report(context.Context) (string, error) {
	return "12 exports attempted, last one failed", errors.New("the collector is not taking this process's data")
}

// finePipeline is a Report with nothing worrying to say.
type finePipeline struct{}

func (finePipeline) Name() string { return "telemetry" }

func (finePipeline) Report(context.Context) (string, error) { return "exported 3s ago", nil }

func serveReports(t *testing.T, checks []health.Check, reports ...health.Report) http.Handler {
	t.Helper()
	_, app := dbtest.Schema(t)
	api, router := httpx.New(httpx.Options{
		Tenants:      sites{tenant: tenancy.Tenant{ID: uuid.New(), Slug: "acme"}},
		Conn:         app,
		Cache:        cache.Memory("pkit"),
		Authorize:    sites{},
		Authenticate: anonymous,
	})
	health.Register(api, checks, reports...)
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the probes are not declared: %v", err)
	}
	return router
}

// TestAReportNeverMovesTheReadinessVerdict: a failing Report leaves /ready answering
// ready, and a failing Check still does not.
func TestAReportNeverMovesTheReadinessVerdict(t *testing.T) {
	router := serveReports(t, nil, downPipeline{})

	got := probe(t, router, tenantHost, "/ready")
	if got.Code != http.StatusOK {
		t.Fatalf("/ready with a down collector and no failing Check = %d, want 200: a replica that is "+
			"serving its tenants is pulled out of the rotation because the trace backend is down", got.Code)
	}
	var body struct {
		Status  string            `json:"status"`
		Reports map[string]string `json:"reports"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatalf("/ready body is not the JSON it has always been: %v (%s)", err, got.Body.String())
	}
	if body.Status != "ok" {
		t.Errorf("/ready says status %q, want ok", body.Status)
	}
	reading, named := body.Reports["telemetry"]
	if !named {
		t.Fatalf("/ready carries no telemetry reading (%s): the operator's whole reason for a Report is "+
			"to see this without it deciding anything", got.Body.String())
	}
	if !strings.Contains(reading, "failed") {
		t.Errorf("the telemetry reading is %q, which does not say the pipeline is down", reading)
	}
	for _, leak := range []string{"http://", "https://", ":4317", "otelcol", "cluster.local"} {
		if strings.Contains(reading, leak) {
			t.Errorf("the /ready body carries %q from the exporter's own error; /ready answers the "+
				"listener a client reaches, anonymously: %q", leak, reading)
		}
	}

	// Liveness is untouched too: it ran no check before and runs no report now.
	if code := probe(t, router, tenantHost, "/health").Code; code != http.StatusOK {
		t.Errorf("/health with a down collector = %d, want 200", code)
	}

	// And the 200 above is a verdict, not a probe that gave up asking: one failing
	// Check on the same handler still refuses, and the refusal is the problem's.
	if code := probe(t, serveReports(t, []health.Check{check{name: "database", err: errors.New("gone")}},
		downPipeline{}), tenantHost, "/ready").Code; code != http.StatusServiceUnavailable {
		t.Errorf("/ready with a failing Check and a failing Report = %d, want 503", code)
	}
}

// TestAReportThatIsContentSaysSo is the other half of the reading: an operator who
// cannot tell "the pipeline is fine" from "there is no pipeline" has been given a
// number they cannot read.
func TestAReportThatIsContentSaysSo(t *testing.T) {
	var body struct {
		Reports map[string]string `json:"reports"`
	}
	got := probe(t, serveReports(t, nil, finePipeline{}), tenantHost, "/ready")
	if got.Code != http.StatusOK {
		t.Fatalf("/ready = %d, want 200: %s", got.Code, got.Body.String())
	}
	if err := json.Unmarshal(got.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode /ready: %v (%s)", err, got.Body.String())
	}
	if reading, ok := body.Reports["telemetry"]; !ok || !strings.Contains(reading, "exported") {
		t.Errorf("the telemetry reading is %q (present %v), want one that says when the last export happened",
			reading, ok)
	}

	// With no Report registered at all, /ready is the two bytes it was before this
	// branch: an existing probe stanza reads the same answer.
	bare := probe(t, serveReports(t, nil), tenantHost, "/ready")
	if bare.Body.String() != `{"status":"ok"}` {
		t.Errorf("/ready with no reports = %s, want the bytes this route has always answered", bare.Body.String())
	}
}
