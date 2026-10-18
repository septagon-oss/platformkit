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

func TestInvalidUserPatchCommitsNoLifecycleOrAttributeChange(t *testing.T) {
	w := compose(t)
	var before *contracts.UserResource
	var beforeEvents []string
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		before, err = w.svc.CreateUser(ctx, tx, contracts.UserDraft{
			UserName: "retained@acme.test", DisplayName: "Original", ExternalID: "directory-id", Active: new(true),
		})
		if err != nil {
			return err
		}
		return tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &beforeEvents).Error
	})
	valid := []contracts.Operation{
		{Op: "replace", Path: "displayName", Value: []byte(`"Changed"`)},
		{Op: "replace", Path: "active", Value: []byte(`false`)},
		{Op: "replace", Path: "externalId", Value: []byte(`"changed-id"`)},
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ops := append(slices.Clone(valid), contracts.Operation{Op: "replace", Path: "unknownAttribute", Value: []byte(`"invalid"`)})
		returned, err := w.svc.PatchUser(ctx, tx, before.ID, ops)
		if !errors.Is(err, crud.ErrInvalid) || returned != nil {
			t.Errorf("invalid PATCH: row=%+v error=%v; want invalid refusal and no row", returned, err)
		}
		return nil
	})
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		after, err := w.svc.GetUser(ctx, tx, before.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			t.Errorf("committed refusal changed person: before=%+v after=%+v", before, after)
		}
		var afterEvents []string
		if err := tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &afterEvents).Error; err != nil {
			return err
		}
		if !slices.Equal(beforeEvents, afterEvents) {
			t.Errorf("committed refusal changed events: %v -> %v", beforeEvents, afterEvents)
		}
		accepted, err := w.svc.PatchUser(ctx, tx, before.ID, valid)
		if err != nil {
			return err
		}
		if accepted.ID != before.ID || accepted.Active || accepted.DisplayName != "Changed" || accepted.ExternalID != "changed-id" {
			t.Errorf("valid PATCH did not apply to the same person: %+v", accepted)
		}
		var acceptedEvents []string
		if err := tx.DB().Table("platformkit_outbox").Order("id").Pluck("id", &acceptedEvents).Error; err != nil {
			return err
		}
		if len(acceptedEvents) <= len(beforeEvents) {
			t.Error("valid PATCH left no additional audit event")
		}
		return nil
	})
}
