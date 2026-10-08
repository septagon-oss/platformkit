package app

// The brief asks for resource attributes service.name,
// pkit.client and pkit.tenant; the delivery refuses the third on purpose and says
// so twice (kit/telemetry's README: "A resource describes the *process*, and this
// process serves many tenants … so a tenant in the resource would be a lie for
// every request but one, or one provider per tenant, which is the unpickable
// singleton the pillar contract refuses"; and resource() below: "No tenant … the
// tenant arrives on the spans, where a request put it").
//
// That refusal is the pillar contract's own line 2 — "Nothing added here is a
// per-process singleton that 0028's shared-instance mode would have to unpick" —
// and until now nothing tested it: grep of this repository's tests for
// service.name / pkit.client returns nothing at all, so the two attributes the
// brief does ask for were untested too, and a later change that reached for the
// obvious place to put a tenant (the resource, where service.name already lives,
// readable by every query in the backend) would have broken the promise silently.
//
// The case therefore pins three things on the pure function that decides them:
// service.name is what the configuration named, pkit.client arrives when and only
// when the deployment named one, and neither tenant key ever arrives — with the
// value the tenant would take put in the configuration by hand, so the assertion
// is about what resource() writes and not about whether the field exists.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/telemetry"
)

func TestTheResourceNamesTheProcessAndItsClientAndNeverTheTenant(t *testing.T) {
	for _, tc := range []struct {
		what   string
		cfg    config.Telemetry
		want   map[string]string
		unwant []string
	}{
		{
			what: "a shared installation, which names no client",
			cfg:  config.Telemetry{ServiceName: "platformkit"},
			want: map[string]string{"service.name": "platformkit"},
			unwant: []string{
				telemetry.AttrClient, telemetry.AttrTenant, telemetry.AttrTenantID,
			},
		},
		{
			what: "a deployment that names the client it serves",
			cfg:  config.Telemetry{ServiceName: "platformkit-worker", Client: "acme"},
			want: map[string]string{"service.name": "platformkit-worker", telemetry.AttrClient: "acme"},
			unwant: []string{
				telemetry.AttrTenant, telemetry.AttrTenantID,
			},
		},
		{
			what: "a client value that is also a tenant slug: the resource says the " +
				"deployment's fact, and the tenant key stays unwritten",
			cfg:  config.Telemetry{ServiceName: "platformkit", Client: "acme"},
			want: map[string]string{telemetry.AttrClient: "acme"},
			unwant: []string{
				telemetry.AttrTenant, telemetry.AttrTenantID,
			},
		},
	} {
		res, err := resource(tc.cfg)
		if err != nil {
			t.Fatalf("%s: resource: %v", tc.what, err)
		}
		have := map[string]string{}
		for _, kv := range res.Attributes() {
			have[string(kv.Key)] = kv.Value.Emit()
		}
		for key, want := range tc.want {
			if got, found := have[key]; !found {
				t.Errorf("%s: the resource carries no %s (has %v)", tc.what, key, keys(have))
			} else if got != want {
				t.Errorf("%s: resource %s = %q, want %q", tc.what, key, got, want)
			}
		}
		for _, key := range tc.unwant {
			if got, found := have[key]; found {
				t.Errorf("%s: the resource carries %s=%q: a resource describes the process, and this "+
					"process serves many tenants, so this key is a lie for every request but one "+
					"(pillar contract 2, 0028's shared-instance mode)", tc.what, key, got)
			}
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTheExportReportNamesThePipelineAndNotTheAddress pins the other half of the
// same promise. kit/app's own comment is explicit about why the exporter's error is
// not the report's text: "The raw error names what it could not reach — 'dial
// otel-collector.observability.svc.cluster.local:4317: connect: connection refused'
// — and /ready answers that on the listener a client reaches, anonymously and for as
// long as the pipeline is down: a service name, a port and a namespace, in the shape
// this repository's own deployment recommends." No case anywhere reads the report
// back (grep for errExportDown answers its two definition lines and nothing else),
// so the redaction was true of the code as written and nothing kept it true.
//
// /ready is served on the client host — measured this round: an anonymous GET to a
// bootstrapped deployment's own tenant host answers
// {"status":"ok","reports":{"telemetry":"exported 0s ago"}} — so the reading is
// public and the address must not be.
func TestTheExportReportNamesThePipelineAndNotTheAddress(t *testing.T) {
	e := &exported{}
	e.note(errors.New("dial otel-collector.observability.svc.cluster.local:4317: connect: connection refused"))
	msg, err := e.Report(context.Background())
	if err == nil {
		t.Errorf("a failed export reports no error at all: %q", msg)
	}
	for _, leak := range []string{"cluster.local", "observability", "4317", "otel-collector", "refused", "dial"} {
		if strings.Contains(msg, leak) {
			t.Errorf("the reading /ready answers on the client host carries %q from the exporter's own "+
				"error: %q (%v)", leak, msg, err)
		}
	}
	if !strings.Contains(msg, "failed") {
		t.Errorf("the reading is %q, which does not say the last export failed", msg)
	}
}
