// Package usertest is the conformance suite for contracts.Service, and a fake
// that passes it.
package usertest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a way to see what was published.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Published is the names of the events published so far, in order. It is
	// what holds an idempotent command to saying nothing the second time.
	Published func() []string
	// Delete is the third door, and the reason it is a closure rather than a
	// method on Service is that deleting a user is generic CRUD: the route is
	// kit/rest's and the floor lives in the Spec's AfterDelete hook, so there
	// is no interface method for the suite to call. Every harness wires it —
	// the real one the way rest.Spec.deleteRow does, the fake its own — because
	// a door tested against one implementation is a door the other can differ
	// on quietly. It is required.
	Delete func(id uuid.UUID) error
	// Granting opens or closes the door that hands out an administering role:
	// false is a caller who may write a user and may not promote anybody. Every
	// harness wires it, because a guard tested against one implementation is a
	// guard the other can skip quietly. See contracts.Granting.
	Granting func(allowed bool)
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	all := cases()
	maps.Copy(all, registrationCases())
	maps.Copy(all, grantingCases())
	maps.Copy(all, verificationCases())
	for name, run := range all {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// good is a password that passes the length rule; short is one that does not.
const (
	good  = "correct horse battery staple"
	short = "hunter2hunt"
)

// Administering is the role this suite's tenant treats as the one that can
// change a role again — the stand-in for a role granting auth's role:manage.
//
// A harness wires its Service to an Administration that answers exactly this
// and nothing else, so the floor cases and the ordinary role cases can share
// one store: every other name in this suite — admin, member, support — grants
// nothing here, which is why granting and ungrunting them is never refused.
const Administering = "owner"

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"invite creates somebody who cannot sign in yet":  inviteUser,
		"an address belongs to one person in a tenant":    reserveAddress,
		"invite refuses something that is not an address": refuseInvalidAddress,
		// What a person asked to stop being held is not answered by the filter that hides
		// them: Delete leaves the row, and Get stops finding it. Retained is the read that
		// sees it, and the only one whose absence would let an erasure certify a person
		// whose address is still in the table.
		"a deleted person is hidden and still held":                                                retainDeletedUser,
		"setting a password makes an invited user active":                                          activateWithPassword,
		"a short password is refused":                                                              refuseShortPassword,
		"setting the same password again is still a password change":                               repeatPasswordChange,
		"roles are granted, normalised, and granted once":                                          normaliseRoles,
		"the last person who can administer the tenant cannot stop":                                preserveLastAdministrator,
		"a second administrator makes the first one ordinary again":                                replaceAdministrator,
		"somebody who has not accepted their invitation does not hold the floor up":                disregardInvitedAdministrator,
		"somebody who never administered can always be removed":                                    removeOrdinaryUser,
		"a tenant whose administrators have all not accepted is refused, and one grant repairs it": repairAdministrationFloor,
		"a role that is not an identifier is refused":                                              refuseInvalidRole,
		"a handle is claimed, folded, and answers to its name":                                     claimHandle,
		"a rename leaves the person where they were":                                               renameHandle,
		"a handle already claimed conflicts without saying whose it is":                            refuseClaimedHandle,
		"names that are not persons are refused, and so is letting one go":                         refuseInvalidHandle,
		"deactivating stops a sign-in, and twice says nothing":                                     deactivateUser,
		"a deactivated user cannot be given a password":                                            refuseDeactivatedPassword,
		"an address finds its person, whatever the case":                                           lookupFoldedAddress,
		"an unknown id is not found":                                                               refuseUnknownUser,
	}
}

// administrator is somebody this tenant can be administered by today: active,
// with a password, holding Administering. Granting the role is never refused —
// the floor is about a grant leaving — and the password is what makes them
// count on the side of the floor that asks who is left. A case that wants
// somebody who has not accepted yet builds them itself.
func administrator(t *testing.T, f Fixture, email string) *contracts.User {
	t.Helper()
	u, err := f.Service.Invite(f.Ctx, f.Tx, email, "")
	if err != nil {
		t.Fatalf("Invite %s: %v", email, err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
		t.Fatalf("SetPassword %s: %v", email, err)
	}
	u, err = f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{Administering})
	if err != nil {
		t.Fatalf("SetRoles %s: %v", email, err)
	}
	return u
}

// deleteDoor fails the case when a harness did not wire Fixture.Delete, rather
// than passing quietly with one of the three doors unexercised.
func deleteDoor(t *testing.T, f Fixture) {
	t.Helper()
	if f.Delete == nil {
		t.Fatal("this harness wired no Fixture.Delete, so the delete door is untested against this implementation")
	}
}

func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}

// grantingCases is the door in front of a promotion: SetRoles asks the Granting
// port only for a write that *adds* a role somebody's table says administers, so
// the cases below are that condition from both sides. They are here, in the
// suite both the fake and the Postgres service run, because the rule is exactly
// the kind that drifts: a guard a little wider refuses the repair the
// last-administrator floor itself tells you to make, and a guard a little
// narrower lets a member promote themself from their own page.
func grantingCases() map[string]func(*testing.T, Fixture) {
	// Somebody the floor will not miss: an active person with no roles, in a
	// tenant where an administrator exists. The guard below is about a promotion,
	// and a promotion is only interesting on a row that is not already one.
	member := func(t *testing.T, f Fixture) *contracts.User {
		t.Helper()
		administrator(t, f, "ada@acme.test")
		u, err := f.Service.Invite(f.Ctx, f.Tx, "ines@acme.test", "")
		if err != nil {
			t.Fatalf("Invite: %v", err)
		}
		if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		return u
	}
	return map[string]func(*testing.T, Fixture){
		"an administering role the caller may not grant is refused, with nothing written": func(t *testing.T, f Fixture) {
			ines := member(t, f)
			f.Granting(false)
			_, err := f.Service.SetRoles(f.Ctx, f.Tx, ines.ID, []string{Administering})
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("SetRoles(administering) = %v, want an ErrInvalid naming the grant", err)
			}
			got, err := f.Service.Get(f.Ctx, f.Tx, ines.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Roles.Has(Administering) {
				t.Errorf("the refused write put %q on the row anyway: %v", Administering, got.Roles)
			}
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventRolesSet,
				contracts.EventInvited, contracts.EventPasswordSet)
		},

		"an ordinary role needs no door": func(t *testing.T, f Fixture) {
			ines := member(t, f)
			f.Granting(false)
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ines.ID, []string{"member"}); err != nil {
				t.Fatalf("a caller who may not promote anybody was refused an ordinary role: %v", err)
			}
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventRolesSet,
				contracts.EventInvited, contracts.EventPasswordSet, contracts.EventRolesSet)
		},

		"taking a role away needs no door": func(t *testing.T, f Fixture) {
			ines := member(t, f)
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ines.ID, []string{"member"}); err != nil {
				t.Fatalf("SetRoles(member): %v", err)
			}
			f.Granting(false)
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ines.ID, nil); err != nil {
				t.Fatalf("a caller who may not promote anybody was refused a bare removal: %v", err)
			}
		},

		"holders is the active people holding one of the named roles": func(t *testing.T, f Fixture) {
			ines := member(t, f)
			got, err := f.Service.Holders(f.Ctx, f.Tx, []string{Administering})
			if err != nil {
				t.Fatalf("Holders: %v", err)
			}
			if len(got) != 1 {
				t.Errorf("Holders = %v, want exactly the one active holder; %s has no roles", got, ines.Email)
			}
		},

		"naming no role names no people": func(t *testing.T, f Fixture) {
			administrator(t, f, "ada@acme.test")
			for _, roles := range [][]string{nil, {}} {
				got, err := f.Service.Holders(f.Ctx, f.Tx, roles)
				if err != nil || len(got) != 0 {
					t.Errorf(`Holders(%v) = %v, %v; want nobody, because "nothing was named" is not "everybody"`, roles, got, err)
				}
			}
		},

		"a person holding two of the named roles is listed once": func(t *testing.T, f Fixture) {
			ines := member(t, f)
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ines.ID, []string{Administering, "secondowner"}); err != nil {
				t.Fatalf("SetRoles: %v", err)
			}
			got, err := f.Service.Holders(f.Ctx, f.Tx, []string{Administering, "secondowner"})
			if err != nil {
				t.Fatalf("Holders: %v", err)
			}
			if len(got) != 2 {
				t.Errorf("Holders = %v, want the two people; a person holding two of the named roles is one notice, not two", got)
			}
		},
	}
}
