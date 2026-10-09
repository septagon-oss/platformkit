package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/pkit"
)

// The served router cannot tell these two claims apart: it asks its TenantLoader
// with httpx.HostOnly(host), which drops the port a listener adds, the case a
// client sent and the trailing dot a fully qualified name carries. A collision
// check that compared the raw spellings instead let the process start with two
// tenants for one address, and the request that arrived chose between them by
// whichever row the loader happened to read first. Two hosts the router can tell
// apart are the other half of the case: the check that refuses one host spelled
// twice must still let one process serve two tenants.
func TestServerRefusesClaimsTheServedRouterCannotTellApart(t *testing.T) {
	cfg := onOneDatabase(t)
	_, err := pkit.NewServer().Config(cfg).Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("collect").Use(doors, desk),
			pkit.Tenant("Acme", "acme.test:8080"), pkit.Tenant("Globex", "acme.test")).
		Build(t.Context())
	if err == nil {
		t.Fatal("Build accepted a second claim that differs from another only by its port")
	}
	for _, want := range []string{"Acme", "Globex", "acme.test"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the port-form refusal never named %s: %v", want, err)
		}
	}

	served, err := pkit.NewServer().Config(cfg).Deploy(buildDeployment(cfg, app.All)).
		Host(pkit.NewApp("collect").Use(doors, desk),
			pkit.Tenant("Acme", "acme.test"), pkit.Tenant("Globex", "globex.test")).
		Build(t.Context())
	if err != nil {
		t.Fatalf("two hosts the router tells apart are two claims: %v", err)
	}
	if err := served.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
