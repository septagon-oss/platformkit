package screens_test

// A guided session reaches a shell as two keys of one entry: `readContract`, which
// names the contract the row speaks, and `resolved.session`, which carries the copy
// and the commands that copy belongs to. ui/screens writes both from two numbers it
// holds apart — the `1` beside `readContract` here and `ContractVersion: 1` inside
// kit/entity/present — and the shell is told both, as `entry.readContract.version`
// and `resolved.session.contractVersion`. Nothing prints either number, or the
// contract's kind, on a body today: the golden records the schema alone. So a bump
// that reached one statement and not the other would tell a phone the words beside
// its contract were written for another contract. Both statements must name one
// number, and a row that is no session must write neither key.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// guidedResource is a session as kit/rest mounts one: a guided read on the resource,
// the session hint on the entry, and both command slots declared.
func guidedResource() httpx.Resource {
	r := resource()
	r.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) {
		return entity.GuidedSession{}, nil
	}
	r.GuidedCommands = [2]bool{true, true}
	r.Present = entity.EntryHints{Session: true}
	return r
}

// entryFields is the entry in the one language a generated client reads: its JSON,
// where an absent key is a promise the shell is written against.
func entryFields(t *testing.T, r httpx.Resource) map[string]any {
	t.Helper()
	body, err := json.Marshal(screens.Describe1(r, true))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

// TestAGuidedEntryStatesItsContractVersionOnce reads the two version statements a
// shell is handed and asks whether they name one contract at one version.
func TestAGuidedEntryStatesItsContractVersionOnce(t *testing.T) {
	fields := entryFields(t, guidedResource())
	contract, _ := fields["readContract"].(map[string]any)
	if contract == nil {
		t.Fatal("a session read publishes no readContract, so no shell can tell which contract to walk")
	}
	if kind, _ := contract["kind"].(string); kind != "guided-session" {
		t.Errorf("the entry says its contract is %q, which no shell knows how to walk", kind)
	}
	policy, _ := fields["resolved"].(map[string]any)
	session, _ := policy["session"].(map[string]any)
	if session == nil {
		t.Fatal("the policy of a guided session carries no session, so the shell is left without the words its screen says")
	}
	version, named := contract["version"].(float64)
	stated, statedToo := session["contractVersion"].(float64)
	if !named || !statedToo || version <= 0 || stated <= 0 {
		t.Fatalf("the entry states its contract as %v and its copy as %v, and a shell needs both to be positive integers", contract["version"], session["contractVersion"])
	}
	if version != stated {
		t.Errorf("the entry names contract %v and its copy contract %v: one contract, one number", version, stated)
	}
}

// TestAResourceThatIsNoSessionWritesNeitherKey holds the other half of the promise:
// those keys are how a shell picks a session out of a catalogue of many rows, so a
// row that is no session may not say it is one.
func TestAResourceThatIsNoSessionWritesNeitherKey(t *testing.T) {
	fields := entryFields(t, resource())
	if got, ok := fields["readContract"]; ok {
		t.Errorf("a note with no session read publishes readContract %v, and a shell that dispatches on it draws a step screen from a note", got)
	}
	policy, _ := fields["resolved"].(map[string]any)
	if got, ok := policy["session"]; ok {
		t.Errorf("a note with no session read carries session copy %v, which a phone offers as a guided walk", got)
	}
}
