// Package topaztest provides a disposable authorizer protocol double.
// It exercises gRPC and protobuf, not a Topaz process or directory.
package topaztest

import (
	"context"
	"github.com/aserto-dev/go-authorizer/aserto/authorizer/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"net"
	"testing"
)

type topazServer struct {
	authorizer.UnimplementedAuthorizerServer
	is func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error)
}

func (s *topazServer) Is(ctx context.Context, request *authorizer.IsRequest) (*authorizer.IsResponse, error) {
	return s.is(ctx, request)
}

// Client starts a local gRPC authorizer and returns its official client plus a
// disconnect function. The test owns server and connection cleanup; is receives
// decoded protobuf requests and supplies independently constructed responses.
func Client(t testing.TB, is func(context.Context, *authorizer.IsRequest) (*authorizer.IsResponse, error)) (authorizer.AuthorizerClient, func()) {
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
	return authorizer.NewAuthorizerClient(connection), func() { _ = connection.Close() }
}
