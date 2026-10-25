package usertest

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	auth "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// InvitationFixture is one authorized actor and the existing service implementation.
// Advance changes the clock shared by commands and reads; Published observes effects.
type InvitationFixture struct {
	Ctx       context.Context
	Tx        db.Tx[db.Tenant]
	Commands  contracts.InvitationCommands
	Reader    contracts.InvitationReader
	Advance   func(time.Duration)
	Published func() []string
	Legacy    func() *contracts.User
}

// RunInvitations applies the same cases to the existing fake and SQL service.
func RunInvitations(t *testing.T, harness func(*testing.T, func(InvitationFixture))) {
	t.Helper()
	for name, test := range map[string]func(*testing.T, InvitationFixture){
		"roles_and_request_commit_together":          invitationCreated,
		"refused_role_has_no_effects":                invitationRoleRefused,
		"replacement_is_revisioned_and_rate_limited": invitationReplaced,
		"revoke_is_terminal":                         invitationEnded,
		"roles_do_not_request_mail":                  invitationEdited,
		"direct_admission_records_provenance":        invitationDirectAdmission,
		"legacy_refusal_does_not_adopt_history":      invitationLegacyRefusal,
		"expiry_is_a_read_and_resend_recovers_it":    invitationExpired,
	} {
		t.Run(name, func(t *testing.T) { harness(t, func(f InvitationFixture) { test(t, f) }) })
	}
}

func invitationLegacyRefusal(t *testing.T, f InvitationFixture) {
	u := f.Legacy()
	before := len(f.Published())
	integration := f.Commands.(auth.InvitationUsers)
	for _, source := range []auth.InvitationSource{{ID: uuid.New()}, {ID: uuid.New(), Kind: "invalid", At: u.CreatedAt}} {
		if err := integration.RequestInvitationLink(f.Ctx, f.Tx, u.ID, source); err == nil {
			t.Fatal("invalid source was accepted")
		}
		if got, err := f.Reader.GetInvitation(f.Ctx, f.Tx, u.ID); got != nil || !errors.Is(err, contracts.ErrInvitationInitializing) {
			t.Fatalf("invalid source adopted history = %+v %v", got, err)
		}
	}
	if got, err := f.Commands.RevokeInvitation(f.Ctx, f.Tx, u.ID, 99); got != nil || !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("stale legacy revoke = %+v %v", got, err)
	}
	if got, err := f.Reader.GetInvitation(f.Ctx, f.Tx, u.ID); got != nil || !errors.Is(err, contracts.ErrInvitationInitializing) {
		t.Fatalf("refusal adopted history = %+v %v", got, err)
	}
	if len(f.Published()) != before {
		t.Fatal("stale legacy command emitted an event")
	}
	r, err := f.Commands.RevokeInvitation(f.Ctx, f.Tx, u.ID, 1)
	if err != nil || r.State != contracts.InvitationRevoked || r.Revision != 2 || r.LegacyAdoptedAt == nil || r.RequestedAt != nil || r.DeliveryID != nil {
		t.Fatalf("legacy revoke = %+v %v", r, err)
	}
}

func invitationExpired(t *testing.T, f InvitationFixture) {
	u := invite(t, f)
	before := len(f.Published())
	f.Advance(time.Hour)
	r, err := f.Reader.GetInvitation(f.Ctx, f.Tx, u.ID)
	if err != nil || r.State != contracts.InvitationExpired || r.Revision != 1 || r.MemberStatus != contracts.StatusInvited {
		t.Fatalf("expiry reading = %+v %v", r, err)
	}
	rows, err := f.Reader.ListInvitations(f.Ctx, f.Tx, contracts.InvitationQuery{State: contracts.InvitationExpired})
	if err != nil || len(rows) != 1 || rows[0].UserID != u.ID {
		t.Fatalf("expiry page = %+v %v", rows, err)
	}
	if err := f.Commands.SetInvitationPassword(f.Ctx, f.Tx, u.ID, 1, "a sufficiently long password"); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("expired admission = %v", err)
	}
	if len(f.Published()) != before {
		t.Fatal("expiry or refused admission emitted an event")
	}
	r, err = f.Commands.ResendInvitation(f.Ctx, f.Tx, u.ID, 1)
	if err != nil || r.State != contracts.InvitationPending || r.Generation != 2 || r.Revision != 2 {
		t.Fatalf("expiry recovery = %+v %v", r, err)
	}
}

func invite(t *testing.T, f InvitationFixture) *contracts.User {
	t.Helper()
	u, err := f.Commands.CreateInvitation(f.Ctx, f.Tx, contracts.InvitationIntent{Email: uuid.NewString() + "@example.com", Roles: []string{"editor"}})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func invitationCreated(t *testing.T, f InvitationFixture) {
	before := len(f.Published())
	u := invite(t, f)
	r, err := f.Reader.GetInvitation(f.Ctx, f.Tx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != contracts.InvitationPending || r.Revision != 1 || r.Generation != 1 || r.DeliveryID == nil || r.MemberStatus != contracts.StatusInvited || !slices.Equal([]string(r.AssignedRoles), []string{"editor"}) {
		t.Fatalf("created = %+v", r)
	}
	if !slices.Equal(f.Published()[before:], []string{contracts.EventInvited, contracts.EventRolesSet}) {
		t.Fatalf("events = %v", f.Published()[before:])
	}
}

func invitationRoleRefused(t *testing.T, f InvitationFixture) {
	before := len(f.Published())
	for _, tc := range []struct {
		role string
		want error
	}{{"admin", contracts.ErrInvitationForbidden}, {"missing", crud.ErrInvalid}} {
		u, err := f.Commands.CreateInvitation(f.Ctx, f.Tx, contracts.InvitationIntent{Email: uuid.NewString() + "@example.com", Roles: []string{tc.role}})
		if !errors.Is(err, tc.want) || u != nil {
			t.Fatalf("%s = %+v %v", tc.role, u, err)
		}
	}
	rows, err := f.Reader.ListInvitations(f.Ctx, f.Tx, contracts.InvitationQuery{})
	if err != nil || len(rows) != 0 || len(f.Published()) != before {
		t.Fatalf("refusal left rows/events: %v %v", rows, err)
	}
}

func invitationReplaced(t *testing.T, f InvitationFixture) {
	u := invite(t, f)
	before := len(f.Published())
	if got, err := f.Commands.ResendInvitation(f.Ctx, f.Tx, u.ID, 1); got != nil {
		t.Fatalf("cooldown returned %+v", got)
	} else if _, ok := errors.AsType[*contracts.InvitationCooldown](err); !ok {
		t.Fatalf("cooldown = %v", err)
	}
	f.Advance(5 * time.Minute)
	r, err := f.Commands.ResendInvitation(f.Ctx, f.Tx, u.ID, 1)
	if err != nil || r.Revision != 2 || r.Generation != 2 || *r.DeliveryID == *u.Invitation.DeliveryID {
		t.Fatalf("replacement = %+v %v", r, err)
	}
	if got, err := f.Commands.ResendInvitation(f.Ctx, f.Tx, u.ID, 1); !errors.Is(err, crud.ErrConflict) || got != nil {
		t.Fatalf("stale = %+v %v", got, err)
	}
	if !slices.Equal(f.Published()[before:], []string{contracts.EventInvitationResent}) {
		t.Fatalf("events = %v", f.Published()[before:])
	}
}

func invitationEnded(t *testing.T, f InvitationFixture) {
	u := invite(t, f)
	r, err := f.Commands.RevokeInvitation(f.Ctx, f.Tx, u.ID, 1)
	if err != nil || r.State != contracts.InvitationRevoked || r.Revision != 2 {
		t.Fatalf("revoke = %+v %v", r, err)
	}
	before := len(f.Published())
	if _, err := f.Commands.RevokeInvitation(f.Ctx, f.Tx, u.ID, 2); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Commands.RevokeInvitation(f.Ctx, f.Tx, u.ID, 1); !errors.Is(err, crud.ErrConflict) || got != nil {
		t.Fatalf("stale no-op = %+v %v", got, err)
	}
	if err := f.Commands.SetInvitationPassword(f.Ctx, f.Tx, u.ID, 2, "a sufficiently long password"); !errors.Is(err, crud.ErrConflict) {
		t.Fatalf("reopen = %v", err)
	}
	if len(f.Published()) != before {
		t.Fatal("terminal retries emitted events")
	}
}

func invitationEdited(t *testing.T, f InvitationFixture) {
	u := invite(t, f)
	before := len(f.Published())
	next, err := f.Commands.SetInvitationRoles(f.Ctx, f.Tx, u.ID, 1, nil)
	if err != nil || next.Invitation.Revision != 2 || next.Invitation.Generation != 1 {
		t.Fatalf("role change = %+v %v", next, err)
	}
	if _, err := f.Commands.SetInvitationRoles(f.Ctx, f.Tx, u.ID, 2, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.Published()[before:], []string{contracts.EventRolesSet}) {
		t.Fatalf("role events = %v", f.Published()[before:])
	}
}

func invitationDirectAdmission(t *testing.T, f InvitationFixture) {
	u := invite(t, f)
	if err := f.Commands.SetInvitationPassword(f.Ctx, f.Tx, u.ID, 1, "short"); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("weak password = %v", err)
	}
	if err := f.Commands.SetInvitationPassword(f.Ctx, f.Tx, u.ID, 1, "a sufficiently long password"); err != nil {
		t.Fatal(err)
	}
	r, err := f.Reader.GetInvitation(f.Ctx, f.Tx, u.ID)
	if err != nil || r.State != contracts.InvitationAccepted || r.AcceptedVia != contracts.InvitationDirectPassword || r.MemberStatus != contracts.StatusActive {
		t.Fatalf("admitted = %+v %v", r, err)
	}
	if got, err := f.Commands.RevokeInvitation(f.Ctx, f.Tx, u.ID, r.Revision); !errors.Is(err, crud.ErrConflict) || got != nil {
		t.Fatalf("accepted revoke = %+v %v", got, err)
	}
}

// InvitationTestAuthority grants the fixture actor editing and the editor role.
// It intentionally withholds admin promotion so the suite can tamper with roles.
type InvitationTestAuthority struct{}

func (InvitationTestAuthority) InspectInvitationAuthority(context.Context, db.Tx[db.Tenant], tenancy.Principal, *contracts.User) (contracts.InvitationAccess, error) {
	return contracts.InvitationAccess{Read: true, Manage: true, KnownRoles: []string{"editor", "admin"}, GrantableRoles: []string{"editor"}, Administering: []string{"admin"}}, nil
}
