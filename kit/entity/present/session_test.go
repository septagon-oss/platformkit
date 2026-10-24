package present_test

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"sync"
	"testing"
)

func TestGuidedPolicyDistinguishesAbsentDeniedAndAllowedCommands(t *testing.T) {
	in := present.Input{Hints: entity.EntryHints{Session: true}, SessionCommands: [2]bool{true, true}}
	denied := present.Resolve(in).Session
	if !denied.Complete.Declared || denied.Complete.Command != nil || !denied.Finish.Declared {
		t.Fatal("denied capability lost its declaration")
	}
	in.Commands = []present.CommandInput{{Verb: "complete-set"}}
	allowed := present.Resolve(in).Session
	if allowed.Complete.Command == nil || *allowed.Complete.Command != "complete-set" || allowed.Finish.Command != nil {
		t.Fatal("callable policy is wrong")
	}
	in.SessionCommands = [2]bool{}
	if absent := present.Resolve(in).Session; absent.Complete.Declared || absent.Complete.Command != nil {
		t.Fatal("undeclared command became callable")
	}
	if present.Resolve(present.Input{}).Session != nil {
		t.Fatal("ordinary entry gained a session")
	}
}

// sessionWords is a screen author's share of the contract: the key each label is
// translated under, beside the English it falls back to.
var sessionWords = map[string]string{
	"screens.session.step": "Step {current} of {total}",
	"screens.session.set":  "Set {current} of {total}",
}

func TestGuidedPolicyTranslationsValidateSlotsAndStayRequestLocal(t *testing.T) {
	var work sync.WaitGroup
	for i := range 20 {
		work.Go(func() {
			in := present.Input{Hints: entity.EntryHints{Session: true}, SessionWords: sessionWords, Text: func(key, fallback string) string {
				switch key {
				case "screens.session.step":
					return "{total} · {current}"
				case "screens.session.set":
					return "{missing}"
				}
				return fallback
			}}
			policy := present.Resolve(in).Session
			if policy.Labels["step"] != "{total} · {current}" || policy.Labels["set"] != "Set {current} of {total}" {
				t.Errorf("invalid translation at %d", i)
			}
			policy.Labels["step"] = "changed"
		})
	}
	work.Wait()
	if plain := present.Resolve(present.Input{Hints: entity.EntryHints{Session: true}, SessionWords: sessionWords}).Session; plain.Labels["step"] != "Step {current} of {total}" {
		t.Fatal("translated response changed registration")
	}
}
