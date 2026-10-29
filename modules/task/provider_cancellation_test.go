package task_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz/topaztest"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/task/contracts/tasktest"
	"google.golang.org/grpc/status"
)

func TestPolicyProvidersRefuseAnAlreadyCancelledEvaluation(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		t.Run(adapter, func(t *testing.T) {
			p := scopePolicy(t, adapter)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			d, err := tenancy.RequirePolicy(ctx, p, scopeRequest())
			if d.Allowed || !errors.Is(err, tenancy.ErrPolicyUnavailable) {
				t.Fatalf("cancelled evaluation = %+v, %v", d, err)
			}
		})
	}
}

func TestPendingPolicyCancellationLeavesNoTaskEffects(t *testing.T) {
	for _, adapter := range []string{"opa", "topaz"} {
		for _, world := range []string{"fake", "sql", "http"} {
			for _, interruption := range []string{"cancel", "caller deadline", "adapter deadline"} {
				if adapter == "opa" && interruption == "adapter deadline" {
					continue
				}
				t.Run(fmt.Sprintf("%s/%s/%s", adapter, world, interruption), func(t *testing.T) {
					entered := make(chan struct{})
					evaluationBound := 4 * time.Second
					if interruption == "adapter deadline" {
						evaluationBound = time.Second
					}
					p := pendingPolicy(t, adapter, entered, evaluationBound)
					base := tasktest.As(tenancy.WithTenant(t.Context(), acme), uuid.New())
					var f tasktest.Fixture
					if world == "fake" {
						fake := tasktest.NewFake()
						fake.Policy = p
						f = tasktest.Fixture{Ctx: base, Service: fake, Published: fake.Events.Names,
							Seed: func(row *contracts.Task) uuid.UUID { return fake.Put(base, row) },
							Task: func(id uuid.UUID) (contracts.Task, error) { return fake.Task(base, id) }}
					} else {
						f = sqlPolicyFixture(t, base, p, world == "http")
					}
					row := f.Seed(&contracts.Task{Title: "cancelled evaluation"})
					before, _ := f.Task(row)
					bound := 5 * time.Second
					if interruption == "caller deadline" {
						bound = 2 * time.Second
					}
					ctx, cancel := context.WithTimeout(base, bound)
					defer cancel()
					type result struct {
						row *contracts.Task
						err error
					}
					done := make(chan result, 1)
					var worker sync.WaitGroup
					defer worker.Wait()
					worker.Go(func() {
						got, err := f.Service.Resolve(ctx, f.Tx, row, "done")
						done <- result{got, err}
					})
					select {
					case <-entered:
					case err := <-done:
						t.Fatalf("command never reached evaluation: %v", err)
					case <-ctx.Done():
						t.Fatal("policy never reached barrier")
					}
					if interruption == "cancel" {
						cancel()
					}
					select {
					case err := <-done:
						if err.err == nil || err.row != nil {
							t.Fatalf("cancelled command returned %+v", err)
						}
					case <-time.After(6 * time.Second):
						t.Fatal("evaluation did not stop")
					}
					after, err := f.Task(row)
					if err != nil || !reflect.DeepEqual(before, after) || len(f.Published()) != 0 {
						t.Fatalf("cancelled command changed task/outbox: %+v %v %v", after, f.Published(), err)
					}
				})
			}
		}
	}
}

func pendingPolicy(t *testing.T, adapter string, entered chan struct{}, evaluationBound time.Duration) tenancy.Policy {
	t.Helper()
	mark := sync.OnceFunc(func() { close(entered) })
	if adapter == "opa" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { mark(); <-r.Context().Done() }))
		t.Cleanup(server.Close)
		p, err := opa.New(t.Context(), "pending.rego", "pending", fmt.Sprintf(`package pending
decision := {"allow": true} if { http.send({"method": "GET", "url": %q, "timeout": "5s"}).status_code == 200 }`, server.URL))
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	client, _ := topaztest.Client(t, func(ctx context.Context, _ *authorizer.IsRequest) (*authorizer.IsResponse, error) {
		mark()
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	})
	p, err := topaz.New(client, topaz.Options{Path: "pending", Revision: "pending-v1", Timeout: evaluationBound})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
