package tenanttest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

func setOIDC(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.SetOIDC(f.Ctx, f.Tx, created.ID, people()); err != nil {
		t.Fatalf("SetOIDC: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventOIDCSet)...)
}

func refusePartialOIDC(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	broken := map[string]func(*contracts.OIDCSettings){
		"no issuer":       func(in *contracts.OIDCSettings) { in.Issuer = "" },
		"a plain host":    func(in *contracts.OIDCSettings) { in.Issuer = "idp acme" },
		"not https":       func(in *contracts.OIDCSettings) { in.Issuer = "http://idp.acme.example" },
		"a query":         func(in *contracts.OIDCSettings) { in.Issuer = "https://idp.acme.example?tenant=acme" },
		"no client":       func(in *contracts.OIDCSettings) { in.ClientID = "" },
		"a secret as ref": func(in *contracts.OIDCSettings) { in.SecretRef = "s3cr3t-value" },
		"an unknown mode": func(in *contracts.OIDCSettings) { in.Registration = "auto" },
		"provision, no role": func(in *contracts.OIDCSettings) {
			in.Registration = contracts.RegistrationProvision
			in.Roles = nil
		},
	}
	for name, breakIt := range broken {
		in := people()
		breakIt(&in)
		if _, err := f.Service.SetOIDC(f.Ctx, f.Tx, created.ID, in); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("SetOIDC with %s = %v, want ErrInvalid", name, err)
		}
	}
	published(t, f, audited(contracts.EventCreated)...)
}

func repeatOIDC(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for range 2 {
		if _, err := f.Service.SetOIDC(f.Ctx, f.Tx, created.ID, people()); err != nil {
			t.Fatalf("SetOIDC: %v", err)
		}
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventOIDCSet)...)
	if _, err := f.Service.SetOIDC(f.Ctx, f.Tx, created.ID, replaced()); err != nil {
		t.Fatalf("SetOIDC with a different issuer: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventOIDCSet, contracts.EventOIDCSet)...)
}

func clearOIDC(t *testing.T, f Fixture) {
	created, err := f.Service.Create(f.Ctx, f.Tx, acme())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := f.Service.ClearOIDC(f.Ctx, f.Tx, created.ID); err != nil {
		t.Fatalf("ClearOIDC of a tenant that never had one: %v", err)
	}
	published(t, f, audited(contracts.EventCreated)...)
	if _, err := f.Service.SetOIDC(f.Ctx, f.Tx, created.ID, people()); err != nil {
		t.Fatalf("SetOIDC: %v", err)
	}
	if _, err := f.Service.ClearOIDC(f.Ctx, f.Tx, created.ID); err != nil {
		t.Fatalf("ClearOIDC: %v", err)
	}
	if _, err := f.Service.ClearOIDC(f.Ctx, f.Tx, created.ID); err != nil {
		t.Fatalf("ClearOIDC again: %v", err)
	}
	published(t, f, audited(contracts.EventCreated, contracts.EventOIDCSet, contracts.EventOIDCCleared)...)
}
