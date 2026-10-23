package authtest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func listSessionRefs(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password, contracts.RoleMember)
	laptop := contracts.Client{UserAgent: "Mozilla/5.0 (laptop)", IP: "203.0.113.1"}
	phone := contracts.Client{UserAgent: "Mozilla/5.0 (phone)", IP: "198.51.100.7"}
	first, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, laptop)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	second, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, phone)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	list, err := f.Service.Sessions(f.Ctx, f.Tx, id, first.ID)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("the list has %d rows, want the two sessions this person has", len(list))
	}
	seen := map[string]*contracts.SessionListing{}
	for _, row := range list {
		if _, err := contracts.SessionRefOf(row.Ref); err != nil {
			t.Errorf("the ref %q is not one this module would issue: %v", row.Ref, err)
		}
		if row.UserAgent == "" || row.IP == "" {
			t.Errorf("%s carries no agent or address: %+v", row.Ref, row)
		}
		seen[row.Ref] = row
	}
	// The entry the person is reading the list from says so, and the other
	// does not: a page cannot warn without it.
	if !seen[contracts.SessionRef(first.ID)].Current {
		t.Errorf("the session making the request is not marked current")
	}
	if seen[contracts.SessionRef(second.ID)].Current {
		t.Errorf("a session other than the caller's is marked current")
	}
	// The property the type exists for: a list a person reads must not be
	// a set of live cookies. The id is what the cookie carries.
	body, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, s := range []*contracts.Session{first, second} {
		if strings.Contains(string(body), s.ID.String()) {
			t.Errorf("the list carries the session id %s, which is a credential", s.ID)
		}
	}
	published(t, f, contracts.EventLoggedIn, contracts.EventLoggedIn)
}

func revokeSessionRef(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password, contracts.RoleMember)
	mine, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	other, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password,
		contracts.Client{UserAgent: "conformance two", IP: "198.51.100.7"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if err := f.Service.RevokeSession(f.Ctx, f.Tx, id, contracts.SessionRef(other.ID)); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
	if _, err := f.Service.Identify(f.Ctx, f.Tx, other.ID, nobody); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("the revoked session still identifies: %v", err)
	}
	if _, err := f.Service.Identify(f.Ctx, f.Tx, mine.ID, nobody); err != nil {
		t.Errorf("the session nobody named is gone too: %v", err)
	}
	if got := sessions(f, id); got != 1 {
		t.Errorf("%d sessions remain, want the one the caller is asking from", got)
	}
	published(t, f, contracts.EventLoggedIn, contracts.EventLoggedIn, contracts.EventSessionRevoked)
}

func revokeAllSessions(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password, contracts.RoleMember)
	mine, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	gone, err := f.Service.RevokeAllSessions(f.Ctx, f.Tx, id)
	if err != nil {
		t.Fatalf("RevokeAllSessions: %v", err)
	}
	if gone != 1 {
		t.Errorf("%d sessions ended, want the one that existed", gone)
	}
	// Unlike ChangePassword's keep there is no exception: "everywhere"
	// includes the machine asking.
	if _, err := f.Service.Identify(f.Ctx, f.Tx, mine.ID, nobody); !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("the caller's own session survived: %v", err)
	}
	if got := sessions(f, id); got != 0 {
		t.Errorf("%d sessions remain, want none", got)
	}
	// Revoking what is already gone is not an error and says nothing: the
	// caller wanted them out and they are.
	if again, err := f.Service.RevokeAllSessions(f.Ctx, f.Tx, id); err != nil || again != 0 {
		t.Errorf("revoking nothing ended %d and returned %v, want 0 and no error", again, err)
	}
	published(t, f, contracts.EventLoggedIn, contracts.EventSessionRevoked)
}

func refuseUnknownSessionRef(t *testing.T, f Fixture) {
	id := f.User("ada@acme.example.com", Password, contracts.RoleMember)
	if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody); err != nil {
		t.Fatalf("Login: %v", err)
	}
	bob := f.User("bob@acme.example.com", Password, contracts.RoleMember)
	_, _, err := f.Service.Login(f.Ctx, f.Tx, "bob@acme.example.com", Password,
		contracts.Client{UserAgent: "somebody else", IP: "198.51.100.9"})
	if err != nil {
		t.Fatalf("Login as somebody else: %v", err)
	}
	list, err := f.Service.Sessions(f.Ctx, f.Tx, bob, uuid.Nil)
	if err != nil {
		t.Fatalf("Sessions: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("somebody else has %d listed sessions, want their own one", len(list))
	}
	// Three shapes of "not yours": never issued, not a ref at all, and
	// another person's here. One answer each, and no write for any of them.
	for _, ref := range []string{contracts.SessionRef(uuid.New()), "not-a-ref", list[0].Ref} {
		if err := f.Service.RevokeSession(f.Ctx, f.Tx, id, ref); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("RevokeSession(%q) = %v, want ErrNotFound", ref, err)
		}
	}
	if got := sessions(f, bob); got != 1 {
		t.Errorf("somebody else holds %d sessions after a stranger tried to revoke them, want 1", got)
	}
	published(t, f, contracts.EventLoggedIn, contracts.EventLoggedIn)
}
