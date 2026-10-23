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
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Served is the set every language case declares before it acts: the default
// first, in the order a negotiation reads. It is a name rather than a literal at
// each call because the two implementations must be handed the same set to be
// comparable, and a case that invented its own could pass by accident.
var Served = tenancy.Languages{Default: "en", Others: []string{"pt-PT"}}

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
	// As is the context of the person named. A self-only command — the one where
	// a person changes their own language — reads who is asking from the request,
	// so a case that wants to be somebody needs a way to become them, and the
	// argument a command does not take cannot be passed by any case. The kernel
	// puts a principal on a context in one place (kit/httpx after authentication);
	// a harness that skipped this field would test the anonymous refusal only.
	As func(userID uuid.UUID) context.Context
	// Serving declares the languages this tenant is served in, which is the set a
	// self-only language command checks its argument against at the moment the
	// write lands. Both harnesses wire it because both decide membership: the
	// service asks the tenant through contracts.LocalePolicy, the fake answers
	// from what it was told. One answer here and not there is a fake that passes
	// a choice its real service would refuse.
	Serving func(l tenancy.Languages)
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
	maps.Copy(all, languageCases())
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

// languageCases is the person's own language: the only command in this contract
// whose subject is the caller. It is in the shared suite rather than only in the
// module's own Postgres tests because both implementations decide it — the service
// asks the tenant through contracts.LocalePolicy and locks the row, the fake keeps
// a map and copies the rules — and a fake that drifted would let every module that
// tests against it pass a choice the shipped service refuses.
func languageCases() map[string]func(*testing.T, Fixture) {
	// The two doors a self-only command needs before it means anything: somebody
	// to be and a set to be served in. A harness that wired neither would run
	// every case below against an anonymous caller and an empty set, and the
	// refusals would look like decisions.
	ready := func(t *testing.T, f Fixture) {
		t.Helper()
		if f.As == nil || f.Serving == nil {
			t.Fatal("this harness wired neither Fixture.As nor Fixture.Serving, so the language command is untested against this implementation")
		}
		f.Serving(Served)
	}
	return map[string]func(*testing.T, Fixture){
		"a person chooses a language this tenant serves": func(t *testing.T, f Fixture) {
			ready(t, f)
			ada := administrator(t, f, "language-choose@acme.test")
			before := len(languageEvents(f))
			got, err := f.Service.SetPreferredLocale(f.As(ada.ID), f.Tx, "pt-PT")
			if err != nil {
				t.Fatalf("SetPreferredLocale: %v", err)
			}
			if got.PreferredLocale != "pt-PT" {
				t.Errorf("preferred locale is %q, want the tag that was asked for", got.PreferredLocale)
			}
			if len(languageEvents(f)) != before+1 {
				t.Errorf("the choice published %v; it is a change and belongs in the trail", contracts.EventLanguageSet)
			}
		},

		"a language this tenant does not serve is refused and nothing is written": func(t *testing.T, f Fixture) {
			ready(t, f)
			ada := administrator(t, f, "language-refused@acme.test")
			before := len(languageEvents(f))
			_, err := f.Service.SetPreferredLocale(f.As(ada.ID), f.Tx, "de")
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("a tag outside the served set = %v, want ErrInvalid", err)
			}
			again, err := f.Service.Get(f.Ctx, f.Tx, ada.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if again.PreferredLocale != "" {
				t.Errorf("the refused choice left %q behind; a refusal writes nothing", again.PreferredLocale)
			}
			if len(languageEvents(f)) != before {
				t.Errorf("the refusal published; nothing changed, so nothing happened")
			}
		},

		"the choice already held publishes nothing": func(t *testing.T, f Fixture) {
			ready(t, f)
			ada := administrator(t, f, "language-held@acme.test")
			who := f.As(ada.ID)
			if _, err := f.Service.SetPreferredLocale(who, f.Tx, "pt-PT"); err != nil {
				t.Fatalf("first SetPreferredLocale: %v", err)
			}
			before := len(languageEvents(f))
			got, err := f.Service.SetPreferredLocale(who, f.Tx, "pt-PT")
			if err != nil {
				t.Fatalf("second SetPreferredLocale: %v", err)
			}
			if got.PreferredLocale != "pt-PT" {
				t.Errorf("preferred locale is %q after the repeat", got.PreferredLocale)
			}
			if len(languageEvents(f)) != before {
				t.Errorf("posting the choice the person already holds published again; an idempotent command says nothing the second time")
			}
		},

		"an empty tag takes the choice back": func(t *testing.T, f Fixture) {
			ready(t, f)
			ada := administrator(t, f, "language-clear@acme.test")
			who := f.As(ada.ID)
			if _, err := f.Service.SetPreferredLocale(who, f.Tx, "pt-PT"); err != nil {
				t.Fatalf("SetPreferredLocale: %v", err)
			}
			before := len(languageEvents(f))
			got, err := f.Service.SetPreferredLocale(who, f.Tx, "")
			if err != nil {
				t.Fatalf("clearing the choice: %v", err)
			}
			if got.PreferredLocale != "" {
				t.Errorf("preferred locale is %q, want the answer back to the browser and the tenant's default", got.PreferredLocale)
			}
			if len(languageEvents(f)) != before+1 {
				t.Errorf("taking the choice back is a change and belongs in the trail")
			}
		},

		"nobody signed in is refused": func(t *testing.T, f Fixture) {
			ready(t, f)
			before := len(languageEvents(f))
			_, err := f.Service.SetPreferredLocale(f.Ctx, f.Tx, "pt-PT")
			if !errors.Is(err, contracts.ErrNoSignIn) {
				t.Errorf("the command with no principal = %v, want ErrNoSignIn", err)
			}
			if len(languageEvents(f)) != before {
				t.Errorf("a command refused for nobody asking published anyway")
			}
		},

		// A session is proof of who somebody was when it was taken, not of what their
		// account is now. The command reads the row it is about to write, so an account
		// closed after the sign-in is refused like any other write that contradicts the
		// row — with 409, which is the verdict that says the account has to be reopened
		// first, and with nothing written and nothing published.
		"a closed account is refused its own language": func(t *testing.T, f Fixture) {
			ready(t, f)
			ada := openAccount(t, f, "language-closed@acme.test")
			who := f.As(ada.ID)
			if _, err := f.Service.SetPreferredLocale(who, f.Tx, "pt-PT"); err != nil {
				t.Fatalf("the open account's own choice: %v", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, ada.ID); err != nil {
				t.Fatalf("Deactivate: %v", err)
			}
			before := len(languageEvents(f))
			row, err := f.Service.SetPreferredLocale(who, f.Tx, "en")
			if !errors.Is(err, contracts.ErrAccountInactive) {
				t.Errorf("a closed account's own command = %v, want the refusal that says so", err)
			}
			if row != nil {
				t.Errorf("the refusal handed back a row (language %q); a refusal returns nothing to read", row.PreferredLocale)
			}
			again, err := f.Service.Get(f.Ctx, f.Tx, ada.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if again.PreferredLocale != "pt-PT" {
				t.Errorf("the closed account's language is %q, want the choice it made while open", again.PreferredLocale)
			}
			if len(languageEvents(f)) != before {
				t.Errorf("a refusal writes nothing, so it publishes nothing")
			}
		},
	}
}

// openAccount is a person who may sign in and holds no role: the subject of a
// case about an account that was open and is not any more, which the last
// administrator's floor would otherwise refuse to close.
func openAccount(t *testing.T, f Fixture, email string) *contracts.User {
	t.Helper()
	u, err := f.Service.Invite(f.Ctx, f.Tx, email, "")
	if err != nil {
		t.Fatalf("Invite %s: %v", email, err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	return u
}

// languageEvents counts the language events published so far, so a case reads its
// own write rather than the invitation and role grant that built the person it is
// about.
func languageEvents(f Fixture) []string {
	if f.Published == nil {
		return nil
	}
	var got []string
	for _, name := range f.Published() {
		if name == contracts.EventLanguageSet {
			got = append(got, name)
		}
	}
	return got
}
