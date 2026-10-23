package usertest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func inviteUser(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "Ada@Acme.Example.com", "  Ada Lovelace  ")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if u.Status != contracts.StatusInvited {
		t.Errorf("status is %q, want %q", u.Status, contracts.StatusInvited)
	}
	if u.Email != "ada@acme.example.com" {
		t.Errorf("email is %q; an address that differs only in case is the same mailbox", u.Email)
	}
	if u.DisplayName != "Ada Lovelace" {
		t.Errorf("display name is %q, want it trimmed", u.DisplayName)
	}
	if u.CanSignIn() {
		t.Error("an invited user with no password can sign in")
	}
	published(t, f, contracts.EventInvited)
}

func reserveAddress(t *testing.T, f Fixture) {
	if _, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", ""); err != nil {
		t.Fatalf("Invite: %v", err)
	}
	_, err := f.Service.Invite(f.Ctx, f.Tx, "ADA@acme.example.com", "")
	if !errors.Is(err, crud.ErrConflict) {
		t.Errorf("a second invitation to the same address = %v, want ErrConflict", err)
	}
	if _, ok := errors.AsType[*crud.UniqueConflict](err); !ok {
		t.Errorf("the duplicate address lost its unique conflict identity: %v", err)
	}
}

func refuseInvalidAddress(t *testing.T, f Fixture) {
	for _, bad := range []string{"", "ada", "ada@", "@acme.example.com", "ada acme@example.com"} {
		if _, err := f.Service.Invite(f.Ctx, f.Tx, bad, ""); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("Invite(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}

func retainDeletedUser(t *testing.T, f Fixture) {
	deleteDoor(t, f)
	ada := administrator(t, f, "ada@acme.example.com")
	administrator(t, f, "grace@acme.example.com")
	if err := f.Delete(ada.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := f.Service.Get(f.Ctx, f.Tx, ada.ID); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("the directory answers a deleted person with %v, want ErrNotFound", err)
	}
	held, err := f.Service.Retained(f.Ctx, f.Tx, ada.ID)
	if err != nil {
		t.Fatalf("Retained: %v", err)
	}
	if held.ID != ada.ID || held.DeletedAt == nil {
		t.Errorf("Retained answered %+v: it is the hidden row, not a live one", held)
	}
	if _, err := f.Service.Retained(f.Ctx, f.Tx, uuid.New()); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Retained for an id that was never anybody = %v, want ErrNotFound", err)
	}
}

func lookupFoldedAddress(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	got, err := f.Service.ByEmail(f.Ctx, f.Tx, "  ADA@Acme.Example.COM ")
	if err != nil || got.ID != u.ID {
		t.Errorf("ByEmail = %v, %v; want the invited user", got, err)
	}
	if _, err := f.Service.ByEmail(f.Ctx, f.Tx, "nobody@acme.example.com"); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("ByEmail of an address nobody has = %v, want ErrNotFound", err)
	}
}

func refuseUnknownUser(t *testing.T, f Fixture) {
	id := uuid.New()
	if _, err := f.Service.Get(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Get of an unknown user = %v, want ErrNotFound", err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, id, good); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("SetPassword on an unknown user = %v, want ErrNotFound", err)
	}
	if _, err := f.Service.Deactivate(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("Deactivate of an unknown user = %v, want ErrNotFound", err)
	}
}
