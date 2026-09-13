package auth_test

import (
	"context"
	"net"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2/api"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
)

func TestTopazCarriesTenantAndActorFactsWithoutDirectoryResolution(t *testing.T) {
	for _, actor := range []tenancy.PolicyActor{
		{Kind: tenancy.PolicyUser, ID: uuid.NewString()},
		{Kind: tenancy.PolicySystem, ID: "billing-worker"},
		{Kind: tenancy.PolicyPublic},
	} {
		t.Run(string(actor.Kind), func(t *testing.T) {
			requests := make(chan *authorizer.IsRequest, 1)
			client := topazClient(t, func(ctx context.Context, request *authorizer.IsRequest) (*authorizer.IsResponse, error) {
				if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 2*time.Second {
					t.Error("evaluation has no bounded default deadline")
				}
				requests <- request
				return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "permit", Is: true}}}, nil
			})
			policy, err := auth.NewTopazPolicy(client, auth.TopazOptions{Path: "platformkit.booking", Decision: "permit", Revision: "configured-v1"})
			if err != nil {
				t.Fatal(err)
			}
			request := topazRequest()
			request.Actor = actor
			request.Resource.Attributes = map[string]any{
				"state": "pending", "amount": 100.0,
				"tenant": "untrusted-attribute", "actor": map[string]any{"kind": "system"},
			}
			decision, err := policy.Decide(t.Context(), request)
			if err != nil || !decision.Allowed || decision.Revision != "configured-v1" || decision.Reason != "permit" {
				t.Fatalf("decision = %+v, error = %v", decision, err)
			}
			wire := <-requests
			identityType := api.IdentityType_IDENTITY_TYPE_MANUAL
			if actor.Kind == tenancy.PolicyPublic {
				identityType = api.IdentityType_IDENTITY_TYPE_NONE
			}
			if wire.IdentityContext.Type != identityType || wire.IdentityContext.Identity != actor.ID {
				t.Fatalf("identity resolution must use the supplied actor: %+v", wire.IdentityContext)
			}
			if wire.PolicyContext.Path != "platformkit.booking" || !reflect.DeepEqual(wire.PolicyContext.Decisions, []string{"permit"}) {
				t.Fatalf("wrong policy selected: %+v", wire.PolicyContext)
			}
			want := map[string]any{
				"tenant": map[string]any{"id": request.Tenant.ID.String(), "slug": "customer", "operator": false},
				"actor":  map[string]any{"kind": string(actor.Kind), "id": actor.ID},
				"action": "booking:approve",
				"resource": map[string]any{
					"tenant_id": request.Tenant.ID.String(), "kind": "booking", "id": "booking-7",
					"attributes": request.Resource.Attributes,
				},
			}
			if got := wire.ResourceContext.AsMap(); !reflect.DeepEqual(got, want) {
				t.Fatalf("policy facts = %#v, want %#v", got, want)
			}
		})
	}
}

func TestTopazDenialIsDistinctFromUnavailableOrInvalidDecisions(t *testing.T) {
	for _, tc := range []struct {
		name      string
		decisions []*authorizer.Decision
		err       error
		wantError bool
	}{
		{name: "denied", decisions: []*authorizer.Decision{{Decision: "allowed", Is: false}}},
		{name: "missing", wantError: true},
		{name: "wrong decision", decisions: []*authorizer.Decision{{Decision: "other", Is: true}}, wantError: true},
		{name: "duplicate decision", decisions: []*authorizer.Decision{{Decision: "allowed", Is: true}, {Decision: "allowed", Is: false}}, wantError: true},
		{name: "unavailable", err: status.Error(codes.Unavailable, "policy service unavailable"), wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := topazClient(t, func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error) {
				return &authorizer.IsResponse{Decisions: tc.decisions}, tc.err
			})
			policy, err := auth.NewTopazPolicy(client, auth.TopazOptions{Path: "platformkit.booking", Revision: "configured-v1"})
			if err != nil {
				t.Fatal(err)
			}
			decision, err := policy.Decide(t.Context(), topazRequest())
			if decision.Allowed || (err != nil) != tc.wantError {
				t.Fatalf("decision = %+v, error = %v", decision, err)
			}
			if tc.err != nil && status.Code(err) != codes.Unavailable {
				t.Fatalf("transport error lost its status: %v", err)
			}
		})
	}
}

func TestTopazRejectsInvalidFactsBeforeCallingTheAuthorizer(t *testing.T) {
	var calls atomic.Int32
	client := topazClient(t, func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error) {
		calls.Add(1)
		return &authorizer.IsResponse{Decisions: []*authorizer.Decision{{Decision: "allowed", Is: true}}}, nil
	})
	policy, err := auth.NewTopazPolicy(client, auth.TopazOptions{Path: "platformkit.booking", Revision: "configured-v1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*tenancy.PolicyRequest)
	}{
		{"another tenant's resource", func(r *tenancy.PolicyRequest) { r.Resource.TenantID = uuid.New() }},
		{"missing identity", func(r *tenancy.PolicyRequest) { r.Actor.ID = "" }},
		{"unsupported attribute", func(r *tenancy.PolicyRequest) { r.Resource.Attributes = map[string]any{"invalid": make(chan int)} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := topazRequest()
			tc.change(&request)
			if decision, err := policy.Decide(t.Context(), request); err == nil || decision.Allowed {
				t.Fatalf("invalid request accepted: decision = %+v, error = %v", decision, err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("invalid facts reached the authorizer")
	}
}

func TestTopazRespectsCancellationAndItsEvaluationDeadline(t *testing.T) {
	client := topazClient(t, func(ctx context.Context, _ *authorizer.IsRequest) (*authorizer.IsResponse, error) {
		<-ctx.Done()
		return nil, status.FromContextError(ctx.Err()).Err()
	})
	for _, tc := range []struct {
		name   string
		cancel bool
		want   codes.Code
	}{
		{"caller cancellation", true, codes.Canceled},
		{"evaluation timeout", false, codes.DeadlineExceeded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := auth.NewTopazPolicy(client, auth.TopazOptions{Path: "platformkit.booking", Revision: "configured-v1", Timeout: 30 * time.Millisecond})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			decision, err := policy.Decide(ctx, topazRequest())
			if decision.Allowed || status.Code(err) != tc.want {
				t.Fatalf("decision = %+v, error = %v, want %s", decision, err, tc.want)
			}
		})
	}
}

func TestTopazRequiresAnExplicitPolicyDeployment(t *testing.T) {
	client := topazClient(t, func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error) {
		return nil, status.Error(codes.Unavailable, "construction must not call Topaz")
	})
	for _, options := range []auth.TopazOptions{
		{Revision: "configured-v1"},
		{Path: "platformkit.task"},
		{Path: "platformkit.task", Revision: "configured-v1", Decision: " "},
		{Path: "platformkit.task", Revision: "configured-v1", Timeout: -time.Second},
	} {
		if _, err := auth.NewTopazPolicy(client, options); err == nil {
			t.Fatalf("accepted incomplete policy deployment: %+v", options)
		}
	}
	if _, err := auth.NewTopazPolicy(nil, auth.TopazOptions{Path: "platformkit.task", Revision: "configured-v1"}); err == nil {
		t.Fatal("accepted a missing client")
	}
}

func topazRequest() tenancy.PolicyRequest {
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "customer"}
	return tenancy.PolicyRequest{
		Tenant:   tenant,
		Actor:    tenancy.PolicyActor{Kind: tenancy.PolicyUser, ID: uuid.NewString()},
		Action:   "booking:approve",
		Resource: tenancy.PolicyResource{TenantID: tenant.ID, Kind: "booking", ID: "booking-7"},
	}
}

type topazServer struct {
	authorizer.UnimplementedAuthorizerServer
	is func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error)
}

func (s *topazServer) Is(ctx context.Context, request *authorizer.IsRequest) (*authorizer.IsResponse, error) {
	return s.is(ctx, request)
}

func topazClient(t *testing.T, is func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error)) authorizer.AuthorizerClient {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	authorizer.RegisterAuthorizerServer(server, &topazServer{is: is})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	connection, err := grpc.NewClient("passthrough:///topaz-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return authorizer.NewAuthorizerClient(connection)
}
