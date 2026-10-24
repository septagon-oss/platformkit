package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz/topaztest"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

type recoveryPolicy struct {
	calls           atomic.Int32
	revoked         atomic.Bool
	allowed, denied tenancy.Policy
}

func (p *recoveryPolicy) Decide(ctx context.Context, r tenancy.PolicyRequest) (tenancy.PolicyDecision, error) {
	p.calls.Add(1)
	if p.revoked.Load() {
		return p.denied.Decide(ctx, r)
	}
	return p.allowed.Decide(ctx, r)
}

func TestPolicySwapKeepsGrantRevocationAndExposesCachedReplyLimits(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		t.Run(adapter, func(t *testing.T) {
			path, cfg := configure(t)
			install(t, path)
			ref := composeReference(cfg, pkit.Development)
			policy := recoveryAdapter(t, adapter)
			application, _, _, _, grants, _, _ := sentence(cfg, catalogues(), "product")
			plan, err := application.Use(productWithPolicy(cfg, grants, admincontracts.Pictures{Source: filecontracts.PictureSource}, policy)).Plan(ref.once)
			if err != nil {
				t.Fatal(err)
			}
			c := ref.composition
			c.options, c.modules = plan.Options(), plan.Modules()
			grants.get = func() []tenancy.Grant { return module.Grants(plan.Modules()) }
			options := appOptions(cfg, c, app.All)
			options.Transport, options.Log = memory.New(), quiet()
			start(t, cfg, c.modules, options)
			admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
			provisionAs(t, cfg, c, acmeTenant(t, cfg), "resolver@acme.localhost", "coordinator")
			actor := signIn(t, cfg, acmeHost, "resolver@acme.localhost", adminPass)
			code, body := do(t, cfg, admin, "POST", acmeHost, tasksPath, `{"title":"recovered resolution"}`)
			if code != 201 {
				t.Fatalf("create = %d %s", code, body)
			}
			resource := tasksPath + "/" + field(t, body, "id")
			resolve := func(key string) (int, string) {
				req, err := http.NewRequestWithContext(t.Context(), "POST", "http://"+cfg.Server.Addr+resource+"/resolve", strings.NewReader(`{"resolution":"done"}`))
				if err != nil {
					t.Fatal(err)
				}
				req.Host = acmeHost
				req.Header.Set("Content-Type", "application/json")
				if key != "" {
					req.Header.Set(httpx.IdempotencyKeyHeader, key)
				}
				res, err := actor.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				body, err := io.ReadAll(res.Body)
				if err != nil {
					t.Fatal(err)
				}
				return res.StatusCode, string(body)
			}
			code, first := resolve("policy-recovery")
			if code != 200 || policy.calls.Load() != 1 {
				t.Fatalf("first resolve = %d %s, calls %d", code, first, policy.calls.Load())
			}
			if n := policyResolutionEvents(t, cfg, field(t, first, "id")); n != 1 {
				t.Fatalf("first resolution emitted %d events", n)
			}
			policy.revoked.Store(true)
			if code, body := resolve(""); code != 403 || !strings.Contains(body, "POLICY_DENIED") || policy.calls.Load() != 2 {
				t.Fatalf("fresh revoked retry = %d %s, calls %d", code, body, policy.calls.Load())
			}
			if code, body := resolve("policy-recovery"); code != 200 || body != first || policy.calls.Load() != 2 {
				t.Fatalf("cached reply characterization = %d %s, calls %d", code, body, policy.calls.Load())
			}
			if n := policyResolutionEvents(t, cfg, field(t, first, "id")); n != 1 {
				t.Fatalf("policy retry emitted %d events", n)
			}
			policy.revoked.Store(false)
			if code, body := do(t, cfg, admin, "PUT", acmeHost, "/api/v1/auth/roles/coordinator", `{"permissions":["task:read"]}`); code != 200 {
				t.Fatalf("revoke grant = %d %s", code, body)
			}
			for _, key := range []string{"", "policy-recovery"} {
				if code, body := resolve(key); code != 403 || !strings.Contains(body, "AUTH_DENIED") || policy.calls.Load() != 2 {
					t.Fatalf("revoked route grant = %d %s, calls %d", code, body, policy.calls.Load())
				}
			}
			if n := policyResolutionEvents(t, cfg, field(t, first, "id")); n != 1 {
				t.Fatalf("grant retry emitted %d events", n)
			}
			if code, body := do(t, cfg, admin, "GET", acmeHost, resource, ""); code != 200 || body != first {
				t.Fatalf("refusal changed task: %d %s; want %s", code, body, first)
			}
		})
	}
}

func recoveryAdapter(t *testing.T, adapter string) *recoveryPolicy {
	t.Helper()
	p := &recoveryPolicy{}
	if adapter == "opa" {
		p.allowed = taskPolicy
		p.denied = opa.MustNew("revoked.rego", "revoked", `package revoked
decision := {"allow": false}`)
	} else {
		client, _ := topaztest.Client(t, func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error) {
			return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "allowed", Is: !p.revoked.Load()}}}, nil
		})
		policy, err := topaz.New(client, topaz.Options{Path: "recovery", Revision: "recovery-v1"})
		if err != nil {
			t.Fatal(err)
		}
		p.allowed, p.denied = policy, policy
		t.Log("Topaz official client over disposable gRPC protocol double; controlled allow/revoke artifact, no directory")
	}
	return p
}

func policyResolutionEvents(t *testing.T, cfg config.Config, taskID string) int {
	t.Helper()
	tenant := tenancy.Tenant{ID: acmeTenant(t, cfg)}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var n int
	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT count(*) FROM platformkit_outbox WHERE name = 'task.resolved' AND payload->>'taskId' = ?", taskID).Scan(&n).Error
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
