package topaz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/topaz/topaztest"
	"google.golang.org/grpc/status"
)

func TestTopazUsesTheEarlierDeadlineAndRefusesAClosedConnection(t *testing.T) {
	for _, tc := range []struct {
		name             string
		caller, provider time.Duration
	}{
		{"caller", time.Second, 3 * time.Second}, {"provider", 3 * time.Second, time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := make(chan time.Duration, 1)
			client := topazClient(t, func(ctx context.Context, _ *authorizer.IsRequest) (*authorizer.IsResponse, error) {
				deadline, ok := ctx.Deadline()
				if !ok {
					t.Error("RPC lost its deadline")
				}
				seen <- time.Until(deadline)
				<-ctx.Done()
				return nil, status.FromContextError(ctx.Err()).Err()
			})
			p, err := topaz.New(client, topaz.Options{Path: "deadline", Revision: "deadline-v1", Timeout: tc.provider})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), tc.caller)
			defer cancel()
			d, err := tenancy.RequirePolicy(ctx, p, topazRequest())
			if d.Allowed || !errors.Is(err, tenancy.ErrPolicyUnavailable) {
				t.Fatalf("deadline = %+v %v", d, err)
			}
			select {
			case bound := <-seen:
				if bound <= 0 || bound > min(tc.caller, tc.provider) {
					t.Fatalf("RPC deadline bound = %v", bound)
				}
			default:
				t.Fatal("RPC never reached the service")
			}
		})
	}
	client, disconnect := topaztest.Client(t, func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error) {
		return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "allowed", Is: true}}}, nil
	})
	p, err := topaz.New(client, topaz.Options{Path: "connection", Revision: "connection-v1"})
	if err != nil {
		t.Fatal(err)
	}
	r := topazRequest()
	if _, err := tenancy.RequirePolicy(t.Context(), p, r); err != nil {
		t.Fatal(err)
	}
	disconnect()
	if d, err := tenancy.RequirePolicy(t.Context(), p, r); d.Allowed || !errors.Is(err, tenancy.ErrPolicyUnavailable) {
		t.Fatalf("closed connection reused allow: %+v, %v", d, err)
	}
}
