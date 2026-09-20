package app

// describe_test.go asks the one question a projection cannot ask of itself:
// whether what it prints could be printed for an application that would not
// start.

import (
	"context"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
)

// TestDescribeRefusesACompositionThatWouldNotBoot is why the description can be
// read as a claim about the wiring and not only about the declarations: Describe
// builds the API through the path Run builds it, so the gate that refuses the
// boot refuses the description with the same words. A description assembled from
// the manifests alone would have printed this composition happily and lied.
func TestDescribeRefusesACompositionThatWouldNotBoot(t *testing.T) {
	cfg, opts := compose(t)
	// ghost guards a route with a permission no module defines: the route is
	// reachable by nobody, and the installation refuses to start over it.
	ghost := module.Module{Name: "ghost", Routes: func(api *httpx.API) {
		httpx.Register(api, huma.Operation{
			OperationID: "haunt", Method: http.MethodGet, Path: "/haunt",
		}, httpx.Permission("ghost:read"), func(context.Context, *struct{}) (*helloOut, error) {
			return &helloOut{}, nil
		})
	}}
	a, err := New(t.Context(), cfg, []module.Module{ghost}, opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the application connection: %v", err)
	}
	defer conn.Close()

	_, err = a.Describe(t.Context(), conn)
	if err == nil || !strings.Contains(err.Error(), "ghost:read") {
		t.Fatalf("Describe = %v, want the refusal the boot makes and the permission it names", err)
	}
	// And it refused without taking the port: a description is not a boot.
	if c, dialErr := net.DialTimeout("tcp", cfg.Server.Addr, time.Second); dialErr == nil {
		_ = c.Close()
		t.Error("Describe listened on the address the application serves on")
	}

	// The refusals New makes are further upstream still, and they need no
	// connection at all: there is no *App to describe until the manifests that
	// every role boots from check out.
	stray := module.Module{Name: "stray", Events: []string{"elsewhere.happened"}}
	if _, err := New(t.Context(), cfg, []module.Module{stray}, opts); err == nil {
		t.Fatal("New accepted an event its own module does not namespace")
	}
}
