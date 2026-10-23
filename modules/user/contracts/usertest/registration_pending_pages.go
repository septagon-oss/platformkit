package usertest

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func pagePendingRegistrations(t *testing.T, f Fixture) {
	var ids []uuid.UUID
	for _, email := range []string{"first@example.com", "second@example.com", "third@example.com"} {
		u, err := f.Service.RegisterPending(f.Ctx, f.Tx, contracts.PendingRegistration{Email: email, Password: good})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, u.ID)
	}
	if _, err := f.Service.ApproveRegistration(f.Ctx, f.Tx, ids[1], uuid.New()); err != nil {
		t.Fatal(err)
	}
	for offset, id := range []uuid.UUID{ids[0], ids[2]} {
		p, err := f.Service.PendingRegistrations(f.Ctx, f.Tx, 1, offset)
		if err != nil || p.Total != 2 || len(p.Items) != 1 || p.Items[0].ID != id {
			t.Fatalf("pending page %d = %+v, %v", offset, p, err)
		}
	}
	for _, bounds := range [][2]int{{-1, 0}, {201, 0}, {1, -1}} {
		if _, err := f.Service.PendingRegistrations(f.Ctx, f.Tx, bounds[0], bounds[1]); !errors.Is(err, crud.ErrInvalid) {
			t.Fatalf("invalid page bounds = %v", err)
		}
	}
}
