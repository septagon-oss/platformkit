package usertest

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func claimHandle(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "Ada Lovelace")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if u.Handle != "" {
		t.Errorf("handle is %q; an invited person has claimed nothing", u.Handle)
	}
	got, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "  Ada  ")
	if err != nil {
		t.Fatalf("SetHandle: %v", err)
	}
	if got.Handle != "ada" {
		t.Errorf("handle is %q, want it trimmed and folded like an address", got.Handle)
	}
	// The whole point of the field: a name typed by a human finds the row.
	found, err := f.Service.ByHandle(f.Ctx, f.Tx, "ADA")
	if err != nil {
		t.Fatalf("ByHandle: %v", err)
	}
	if found.ID != u.ID {
		t.Errorf("ByHandle found %s, want %s", found.ID, u.ID)
	}
	published(t, f, contracts.EventInvited, contracts.EventHandleSet)
}

func renameHandle(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err = f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "ada"); err != nil {
		t.Fatalf("SetHandle: %v", err)
	}
	again, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "ada.lovelace")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	// The claim this design rests on: the handle is an alias and the uuid is
	// the key, so a rename rewrites nothing and every event, foreign key and
	// audit row naming the old subject still names the same person.
	if again.ID != u.ID {
		t.Errorf("a rename moved the person: %s became %s", u.ID, again.ID)
	}
	if _, err := f.Service.ByHandle(f.Ctx, f.Tx, "ada"); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("the released handle still resolves: %v", err)
	}
	// Two claims, two events: who held `ada` before is the question a rename
	// raises, and it has to have an answer in the trail.
	published(t, f, contracts.EventInvited, contracts.EventHandleSet, contracts.EventHandleSet)
}

func refuseClaimedHandle(t *testing.T, f Fixture) {
	ada, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "Ada Lovelace")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	grace, err := f.Service.Invite(f.Ctx, f.Tx, "grace@acme.example.com", "Grace Hopper")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err = f.Service.SetHandle(f.Ctx, f.Tx, ada.ID, "grace"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	_, err = f.Service.SetHandle(f.Ctx, f.Tx, grace.ID, "grace")
	if !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("second claim = %v, want ErrConflict", err)
	}
	// The answer must not confirm who is in this tenant to somebody probing
	// for it: the rule, never the holder.
	for _, leak := range []string{"ada@acme.example.com", "Ada Lovelace", ada.ID.String()} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the conflict names %q; it may name the rule only: %v", leak, err)
		}
	}
}

func refuseInvalidHandle(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	for _, refused := range []string{"admin", "root", "support", "me", "signin", "null", "platformkit"} {
		if _, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, refused); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("SetHandle(%q) = %v, want ErrInvalid: it names the platform or a door, not a person", refused, err)
		}
	}
	for _, malformed := range []string{"ad", "a b", "ada!", "_ada", "ada.", strings.Repeat("a", 33)} {
		if _, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, malformed); !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("SetHandle(%q) = %v, want ErrInvalid", malformed, err)
		}
	}
	// Released is refused: a name somebody can drop while still being called
	// it is a name two people can be answered to.
	if _, err = f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "ada"); err != nil {
		t.Fatalf("SetHandle: %v", err)
	}
	if _, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, ""); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("releasing a handle = %v, want ErrInvalid", err)
	}
	// Every refusal above changed nothing, so exactly one event was published.
	published(t, f, contracts.EventInvited, contracts.EventHandleSet)
}
