package internal_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/scim"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

// TestErasureEmptiesOnlyTheIdentifiersThatNameThePersonInTheRequestTenant checks the
// erasure's rule for a column the table holds unique: every identifier of the request
// tenant that carries the person's address in any spelling is emptied, the rows that held
// them stay, an identifier the provider invented for itself is kept, another tenant's
// identical identifier is kept, and running the erasure again changes nothing.
func TestErasureEmptiesOnlyTheIdentifiersThatNameThePersonInTheRequestTenant(t *testing.T) {
	w := compose(t)
	part := scim.PersonalData()
	const (
		email    = "provisioned.person@example.com"
		spelled  = "Provisioned.Person@Example.com"
		quoted   = "acme:provisioned.person@example.com"
		invented = "00u1a2b3c4"
	)
	var person, quoting, unrelated, foreign *contracts.UserResource
	var group *contracts.GroupResource
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		if person, err = w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: email,
			DisplayName: "Provisioned Person", ExternalID: spelled, Active: new(true),
		}); err != nil {
			return err
		}
		if quoting, err = w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "colleague@example.com",
			DisplayName: "Other Person", ExternalID: quoted, Active: new(true),
		}); err != nil {
			return err
		}
		if unrelated, err = w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: "third@example.com",
			DisplayName: "Third Person", ExternalID: invented, Active: new(true),
		}); err != nil {
			return err
		}
		if group, err = w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{
			Schemas: []string{contracts.SchemaGroup}, DisplayName: "Night Crew",
			Members: []contracts.MemberRef{{Value: person.ID.String()}},
		}); err != nil {
			return err
		}
		return w.svc.DeactivateUser(ctx, tx, person.ID)
	})
	w.inTenant(t, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		foreign, err = w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			Schemas: []string{contracts.SchemaUser}, UserName: email,
			DisplayName: "Provisioned Person", ExternalID: spelled,
		})
		return err
	})

	subject := personaldata.Subject{ID: person.ID, Email: email, DisplayName: "Provisioned Person"}
	for range 2 {
		w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return part.Data.Erase(ctx, tx, subject)
		})
	}

	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		raw, err := part.Data.Export(ctx, tx, subject)
		if err != nil {
			return err
		}
		var own struct {
			ExternalID          string               `json:"externalId"`
			ProviderDeactivated bool                 `json:"providerDeactivated"`
			Groups              []contracts.GroupRef `json:"groups"`
		}
		if err := json.Unmarshal(raw, &own); err != nil {
			return err
		}
		if own.ExternalID != "" || !own.ProviderDeactivated || len(own.Groups) != 1 || own.Groups[0].Value != group.ID.String() {
			t.Errorf("the person's provisioning record after erasure = %+v, want no identifier, still stood down, still in %s",
				own, group.ID)
		}
		for _, want := range []struct {
			who        *contracts.UserResource
			userName   string
			externalID string
		}{
			{who: quoting, userName: "colleague@example.com", externalID: ""},
			{who: unrelated, userName: "third@example.com", externalID: invented},
		} {
			held, err := w.svc.GetUser(ctx, tx, want.who.ID)
			if err != nil {
				return err
			}
			if held.UserName != want.userName || held.DisplayName != want.who.DisplayName || held.ExternalID != want.externalID {
				t.Errorf("colleague %s after erasure = userName %q, displayName %q, externalId %q; want %q, %q, %q",
					want.who.ID, held.UserName, held.DisplayName, held.ExternalID,
					want.userName, want.who.DisplayName, want.externalID)
			}
		}
		return nil
	})
	w.inTenant(t, globex, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		held, err := w.svc.GetUser(ctx, tx, foreign.ID)
		if err != nil {
			return err
		}
		if held.ExternalID != spelled {
			t.Errorf("another tenant's identifier after erasure = %q, want %q", held.ExternalID, spelled)
		}
		return nil
	})
}
