package saved

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/saved/contracts"
)

// TestThePersonalDataClaimMatchesTheSQLThisModuleOwns runs the kernel's own
// reading of the claim — the one boot runs over a composed module — against these
// migrations: the person column is the one the SQL carries, every other uuid is
// filed as somebody else's row, and nothing is claimed about a column that is not
// there. A claim that has drifted from the file that earned it is the failure a
// person's erasure cannot survive.
func TestThePersonalDataClaimMatchesTheSQLThisModuleOwns(t *testing.T) {
	bad := manifest().PersonalData.Check("saved", Migrations.Files)
	for _, problem := range bad {
		t.Error(problem)
	}
}

// TestTheManifestPromisesWhatTheContractsPackageNames keeps the two lists the
// kernel reads — the grants and the events — pointing at the constants every
// command and every publisher actually uses, each event with the payload type the
// outbox checks a body against.
func TestTheManifestPromisesWhatTheContractsPackageNames(t *testing.T) {
	m := manifest()
	if m.Name != "saved" {
		t.Errorf("manifest names itself %q", m.Name)
	}
	if m.Migrations == nil {
		t.Error("the manifest carries no SQL, so a composition would migrate nothing")
	}
	if len(m.Permissions) != 2 {
		t.Fatalf("grants: %d, want the read and the write the service asks for", len(m.Permissions))
	}
	keys := map[string]bool{}
	for _, granted := range m.Permissions {
		if granted.Label == "" || granted.Label == granted.Key {
			t.Errorf("grant %q is labelled %q, which a refusal cannot show a person", granted.Key, granted.Label)
		}
		keys[granted.Key] = true
	}
	if !keys[contracts.PermissionRead] || !keys[contracts.PermissionWrite] {
		t.Errorf("grants %v are not the two the commands ask for", m.Permissions)
	}
	emitted := m.Emits()
	if len(emitted) != 2 {
		t.Fatalf("emits %v, want the save and the removal", emitted)
	}
	for _, event := range emitted {
		if event.Payload == nil {
			t.Errorf("event %q carries no payload type, so the outbox would check nothing", event.Name)
		}
	}
}
