package screens_test

// The walk is several screens, not one: a step, the rest screen that follows a set, the
// finishing summary. Each hands the countdown script the same config field, `scope`, and the
// script keys every stored countdown by it — a step's named timer, and the pause a person
// extends by fifteen seconds on the rest screen are stored at one address made of that scope,
// the session and the step. So the scope has to be the same string on every screen of one
// person's walk: a rest screen that handed the script any other one would store the extended
// pause where no step looks for it, and two people walking one session in one browser would
// share the address a pause is stored at. The pair page's scope is pinned beside this case;
// the rest page's was read by nothing.
// This case asks one caller for the step and for the rest that follows its set, reads the
// scope each document writes the way the script does, and asks that both name that caller and
// that the two answers be one string.

import (
	"testing"

	"github.com/google/uuid"
)

// TestTheGuidedRestPageCarriesItsStepCountdownScope asks one caller for a step and for the rest
// screen that follows its set, and compares the countdown scope each page hands the script. Both
// name the tenant, that caller and this resource; they are equal, so a pause extended on the rest
// screen is the countdown its step resumes. A rest page that named the walk without the caller
// would store one person's pause where another person's step reads it.
func TestTheGuidedRestPageCarriesItsStepCountdownScope(t *testing.T) {
	session := oneSetRecorded()
	h := scopeServer(t, session)
	caller := uuid.MustParse("55555555-5555-4555-8555-555555555555")
	step := session.Steps[0]
	root := "/app/note/notes/" + session.ID.String()
	pair, _ := guidedScopeAnswered(t, h, root+"/steps/"+step.ID.String()+
		"/sets/"+step.Sets[0].String(), caller)
	rest, restBody := guidedScopeAnswered(t, h, root+"/rest/"+step.ID.String()+
		"/"+step.Sets[0].String(), caller)
	res := resource()
	if want := formTenant + "/" + caller.String() + "/" + res.Module + "/" + res.Entity; rest != want {
		t.Errorf("the rest page answered to %s hands its countdown store %q, want %q — the same address its "+
			"step stores under, so a pause extended here is the countdown the step resumes: %s",
			caller, rest, want, firstWords(restBody))
	}
	if rest != pair {
		t.Errorf("the rest page stores its pause under %q while the step it follows stores under %q, so the "+
			"pause a person adds on the rest screen is read back by no step: %s", rest, pair, firstWords(restBody))
	}
}
