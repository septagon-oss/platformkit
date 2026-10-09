package internal_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/scim/contracts"
)

func TestConcurrentGroupRemovalsRevokeBothRoles(t *testing.T) {
	w := compose(t, "finance", "engineering")
	var person uuid.UUID
	var groups []uuid.UUID
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := w.svc.CreateUser(ctx, tx, contracts.UserDraft{UserName: "person@acme.test", Active: new(true)})
		if err != nil {
			return err
		}
		person = u.ID
		for _, role := range []string{"finance", "engineering"} {
			g, err := w.svc.CreateGroup(ctx, tx, contracts.GroupDraft{DisplayName: role, Members: []contracts.MemberRef{{Value: person.String()}}})
			if err != nil {
				return err
			}
			groups = append(groups, g.ID)
			if _, err := w.svc.MapGroup(mayMap(ctx), tx, role, role); err != nil {
				return err
			}
		}
		return nil
	})
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 30*time.Second)
	defer cancel()
	started := make(chan int, 2)
	done := make(chan error, 2)
	// An independent writer holds the person's row. Both group commands must
	// wait on that row before they can commit their derived role assignments.
	workerContext := ctx
	err := db.Run(ctx, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", person).Error; err != nil {
			return err
		}
		for _, group := range groups {
			go func() {
				done <- db.Run(workerContext, w.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					var pid int
					if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
						return err
					}
					started <- pid
					_, err := w.svc.PatchGroup(ctx, tx, group, []contracts.Operation{{Op: "remove", Path: "members"}})
					return err
				})
			}()
		}
		var pids []int
		for range 2 {
			select {
			case pid := <-started:
				pids = append(pids, pid)
			case err := <-done:
				return fmt.Errorf("writer ended before waiting: %v", err)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			var blocked int64
			if err := tx.DB().Raw("SELECT count(*) FROM pg_stat_activity WHERE pid IN ? AND wait_event_type = 'Lock'", pids).Scan(&blocked).Error; err != nil {
				return err
			}
			if blocked == 2 {
				return nil
			}
			select {
			case err := <-done:
				return fmt.Errorf("writer ended before lock release: %v", err)
			case <-ticker.C:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	w.inTenant(t, acme, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, group := range groups {
			g, err := w.svc.GetGroup(ctx, tx, group)
			if err != nil {
				return err
			}
			if len(g.Members) != 0 {
				t.Errorf("group %s still has members: %+v", group, g.Members)
			}
		}
		u, err := w.users.Get(ctx, tx, person)
		if err != nil {
			return err
		}
		if len(u.Roles) != 0 {
			t.Errorf("both memberships removed, but concurrent commands retained roles %v", u.Roles)
		}
		return nil
	})
}
