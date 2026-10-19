package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestMembershipCannotCommitBehindAConcurrentGroupDeletion(t *testing.T) {
	w := compose(t)
	var group, person uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "member@acme.test"})
		if err != nil {
			return err
		}
		person = u.ID
		g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: "People"})
		if err != nil {
			return err
		}
		group = g.ID
		return nil
	})
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 15*time.Second)
	defer cancel()
	type answer struct {
		row *contracts.GroupResource
		err error
	}
	done := make(chan answer, 1)
	var early *answer
	// Hold a completed delete uncommitted while a second transaction tries to
	// add membership. A correct lock or FK makes the second writer wait/refuse.
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := w.svc.DeleteGroup(ctx, tx, group); err != nil {
			return err
		}
		started := make(chan struct{})
		go func() {
			var row *contracts.GroupResource
			err := db.Run(tenancy.WithTenant(t.Context(), acme), w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				close(started)
				var err error
				row, err = w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{Op: "add", Path: "members", Value: []byte(`[{"value":"` + person.String() + `"}]`)}})
				return err
			})
			done <- answer{row, err}
		}()
		select {
		case <-started:
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case got := <-done:
			early = &got
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var got answer
	if early != nil {
		got = *early
	} else {
		select {
		case got = <-done:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if got.err != nil && got.row != nil {
		t.Errorf("refused membership returned a stale group: %+v", got.row)
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := w.svc.GetGroup(ctx, tx, group); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("deleted group read: %v", err)
		}
		var count int64
		if err := tx.DB().Table("scim_group_members").Where("group_id = ?", group).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			t.Errorf("concurrent add committed %d orphan membership(s) behind deleted group; writer error=%v", count, got.err)
		}
		return nil
	})
}
