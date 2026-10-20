package internal_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestInvalidReactivationPatchPreservesTheConnectorsRightToReactivate(t *testing.T) {
	w := compose(t)
	var before *contracts.UserResource
	var beforeEvents []string
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		person, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			UserName: "returning@acme.test", DisplayName: "Original", ExternalID: "directory-id", Active: new(true),
		})
		if err != nil {
			return err
		}
		if err := w.svc.DeactivateUser(ctx, tx, person.ID); err != nil {
			return err
		}
		before, err = w.svc.GetUser(ctx, tx, person.ID)
		if err != nil {
			return err
		}
		if before.Active {
			t.Fatal("connector deactivation left the person active")
		}
		return tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &beforeEvents).Error
	})
	valid := []contracts.Operation{{Op: "replace", Value: []byte(`{"active":true,"displayName":"Returned","externalId":"returned-id"}`)}}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ops := append(slices.Clone(valid), contracts.Operation{Op: "replace", Path: "unknownAttribute", Value: []byte(`"invalid"`)})
		returned, err := w.svc.PatchUser(ctx, tx, before.ID, ops)
		if !errors.Is(err, crud.ErrInvalid) || returned != nil {
			t.Errorf("invalid reactivation: row=%+v error=%v; want invalid refusal and no row", returned, err)
		}
		return nil
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		after, err := w.svc.GetUser(ctx, tx, before.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			t.Errorf("committed refusal changed inactive identity: before=%+v after=%+v", before, after)
		}
		var afterEvents []string
		if err := tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &afterEvents).Error; err != nil {
			return err
		}
		if !slices.Equal(beforeEvents, afterEvents) {
			t.Errorf("committed refusal changed event identities: %v -> %v", beforeEvents, afterEvents)
		}
		accepted, err := w.svc.PatchUser(ctx, tx, before.ID, valid)
		if err != nil {
			return err
		}
		if accepted.ID != before.ID || !accepted.Active || accepted.DisplayName != "Returned" || accepted.ExternalID != "returned-id" {
			t.Errorf("valid reactivation failed to restore the same person: %+v", accepted)
		}
		var acceptedEvents []string
		if err := tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &acceptedEvents).Error; err != nil {
			return err
		}
		if len(acceptedEvents) <= len(beforeEvents) {
			t.Error("valid reactivation left no additional audit event")
		}
		return nil
	})
}
