package contracts_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func invitationSubject(t *testing.T) (*contracts.User, contracts.InvitationTiming, time.Time) {
	t.Helper()
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	timing := contracts.InvitationTiming{Lifetime: time.Hour, ResendInterval: 5 * time.Minute}
	u := &contracts.User{Email: "person@example.com", Status: contracts.StatusInvited, Roles: contracts.Roles{"editor"}}
	u.ID = uuid.New()
	h, err := contracts.BeginInvitation(u, timing, now, uuid.New(), false)
	if err != nil {
		t.Fatal(err)
	}
	u.Invitation = h
	return u, timing, now
}

func TestInvitationRevisionPrecedesNoOp(t *testing.T) {
	u, timing, at := invitationSubject(t)
	for _, revision := range []int64{-1, 0, 2} {
		want := crud.ErrConflict
		if revision <= 0 {
			want = crud.ErrInvalid
		}
		got, changed, err := contracts.DecideInvitation(u, contracts.InvitationMutation{Action: contracts.InvitationRoles, ExpectedRevision: revision, Roles: []string{"editor"}}, timing, at)
		if !errors.Is(err, want) || got != nil || changed {
			t.Fatalf("revision %d: %+v, %v, %v", revision, got, changed, err)
		}
	}
}

func TestInvitationResendReplacesOnlyAfterCooldown(t *testing.T) {
	u, timing, at := invitationSubject(t)
	before := *u
	delivery := uuid.New()
	intent := contracts.InvitationMutation{Action: contracts.InvitationResend, ExpectedRevision: 1, DeliveryID: delivery}
	got, changed, err := contracts.DecideInvitation(u, intent, timing, at.Add(5*time.Minute-time.Microsecond))
	cooldown, ok := errors.AsType[*contracts.InvitationCooldown](err)
	if !ok || !cooldown.RetryAt.Equal(at.Add(5*time.Minute)) || got != nil || changed {
		t.Fatalf("cooldown: %+v %v %v", got, changed, err)
	}
	got, changed, err = contracts.DecideInvitation(u, intent, timing, at.Add(5*time.Minute))
	if err != nil || !changed || got.Invitation.Generation != 2 || got.Invitation.Revision != 2 || *got.Invitation.DeliveryID != delivery {
		t.Fatalf("replacement: %+v %v %v", got, changed, err)
	}
	if !reflect.DeepEqual(*u, before) {
		t.Fatal("decision changed its input")
	}
	accepted, _, err := contracts.DecideInvitation(got, contracts.InvitationMutation{Action: contracts.InvitationPasswordLink, Generation: 1}, timing, at.Add(6*time.Minute))
	if !errors.Is(err, crud.ErrConflict) || accepted != nil {
		t.Fatalf("obsolete acceptance: %+v %v", accepted, err)
	}
}

func TestInvitationExpiryDoesNotMoveRevision(t *testing.T) {
	u, _, at := invitationSubject(t)
	for _, tc := range []struct {
		at   time.Time
		want string
	}{
		{at.Add(time.Hour - time.Microsecond), contracts.InvitationPending},
		{at.Add(time.Hour), contracts.InvitationExpired},
		{at.Add(2 * time.Hour), contracts.InvitationExpired},
	} {
		if got := contracts.InvitationState(u.Invitation, tc.at); got != tc.want {
			t.Fatalf("state = %s, want %s", got, tc.want)
		}
	}
	if u.Invitation.Revision != 1 {
		t.Fatal("read moved revision")
	}
}

func TestInvitationRevokeIsTerminalAndIdempotent(t *testing.T) {
	u, timing, at := invitationSubject(t)
	got, changed, err := contracts.DecideInvitation(u, contracts.InvitationMutation{Action: contracts.InvitationRevoke, ExpectedRevision: 1}, timing, at)
	if err != nil || !changed || got.Status != contracts.StatusInactive || got.Invitation.Revision != 2 {
		t.Fatalf("revoke: %+v %v %v", got, changed, err)
	}
	again, changed, err := contracts.DecideInvitation(got, contracts.InvitationMutation{Action: contracts.InvitationRevoke, ExpectedRevision: 2}, timing, at.Add(time.Minute))
	if err != nil || changed || !reflect.DeepEqual(got, again) {
		t.Fatalf("repeat: %+v %v %v", again, changed, err)
	}
	for _, action := range []string{contracts.InvitationPasswordLink, contracts.InvitationDirectPassword, contracts.InvitationIdentityProvider, contracts.InvitationResend, contracts.InvitationRoles} {
		answer, _, err := contracts.DecideInvitation(got, contracts.InvitationMutation{Action: action, ExpectedRevision: 2, Generation: 1, DeliveryID: uuid.New()}, timing, at.Add(time.Hour))
		if !errors.Is(err, crud.ErrConflict) || answer != nil {
			t.Fatalf("%s reopened: %+v %v", action, answer, err)
		}
	}
}

func TestInvitationAdmissionPreservesRolesAndProvenance(t *testing.T) {
	for _, via := range []string{contracts.InvitationPasswordLink, contracts.InvitationDirectPassword, contracts.InvitationIdentityProvider} {
		u, timing, at := invitationSubject(t)
		got, changed, err := contracts.DecideInvitation(u, contracts.InvitationMutation{Action: via, ExpectedRevision: 1, Generation: 1}, timing, at)
		if err != nil || !changed || got.Status != contracts.StatusActive || (got.Invitation.AcceptedVia == nil || *got.Invitation.AcceptedVia != via) || !reflect.DeepEqual(got.Roles, u.Roles) {
			t.Fatalf("%s: %+v %v %v", via, got, changed, err)
		}
		got, _, err = contracts.DecideInvitation(u, contracts.InvitationMutation{Action: via, ExpectedRevision: 1, Generation: 1}, timing, at.Add(time.Hour))
		if !errors.Is(err, crud.ErrConflict) || got != nil {
			t.Fatalf("expired %s: %+v %v", via, got, err)
		}
	}
}

func TestInvitationRoleRemovalDoesNotRequireRegrant(t *testing.T) {
	access := contracts.InvitationAccess{KnownRoles: []string{"editor", "admin"}}
	if err := contracts.CheckInvitationRoles([]string{"editor", "admin"}, []string{"editor"}, access); err != nil {
		t.Fatal(err)
	}
	if err := contracts.CheckInvitationRoles(nil, []string{"editor"}, access); !errors.Is(err, contracts.ErrInvitationForbidden) {
		t.Fatalf("overgrant: %v", err)
	}
	if err := contracts.CheckInvitationRoles(nil, []string{"missing"}, access); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("unknown: %v", err)
	}
}

func TestInvitationFloorPreservesOnlyTheLastAppointment(t *testing.T) {
	u, _, _ := invitationSubject(t)
	u.Roles = contracts.Roles{"admin"}
	after := *u
	after.Status = contracts.StatusInactive
	other := *u
	other.ID = uuid.New()
	if err := contracts.CheckInvitationAdministration(u, &after, []string{"admin"}, []*contracts.User{&other}); err != nil {
		t.Fatal(err)
	}
	if err := contracts.CheckInvitationAdministration(u, &after, []string{"admin"}, nil); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("last appointment: %v", err)
	}
	u.Status = contracts.StatusActive
	if err := contracts.CheckInvitationAdministration(u, &after, []string{"admin"}, []*contracts.User{&other}); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("invited successor held active floor: %v", err)
	}
}

func TestInvitationMutationsRequireTheComposedTiming(t *testing.T) {
	u, _, at := invitationSubject(t)
	for _, action := range []string{contracts.InvitationPasswordLink, contracts.InvitationDirectPassword, contracts.InvitationIdentityProvider, contracts.InvitationRevoke, contracts.InvitationRoles, contracts.InvitationResend} {
		got, changed, err := contracts.DecideInvitation(u, contracts.InvitationMutation{Action: action, ExpectedRevision: 1, Generation: 1}, contracts.InvitationTiming{}, at)
		if got != nil || changed || !errors.Is(err, contracts.ErrInvitationUnavailable) {
			t.Fatalf("unconfigured %s: %+v %v %v", action, got, changed, err)
		}
	}
}
