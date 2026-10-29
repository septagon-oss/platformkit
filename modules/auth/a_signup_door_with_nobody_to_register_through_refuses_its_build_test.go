package auth_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/auth"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/pkit"
)

// noEmailRegistrar and noPendingRegistrar stand in for the composition that
// names a signup door while supplying nobody to write through: each declares the
// contract the door reads and puts no implementation of it, which is the one
// shape a plan can answer and a hand-written literal cannot.
var noEmailRegistrar = pkit.NewModule("noemailregistrar", func(w *pkit.Wiring) (module.Module, error) {
	pkit.Put[usercontracts.EmailRegistrar](w, nil)
	return module.Module{Name: "noemailregistrar"}, nil
}, pkit.Provides[usercontracts.EmailRegistrar]())

var noPendingRegistrar = pkit.NewModule("nopendingregistrar", func(w *pkit.Wiring) (module.Module, error) {
	pkit.Put[usercontracts.PendingRegistrar](w, nil)
	return module.Module{Name: "nopendingregistrar"}, nil
}, pkit.Provides[usercontracts.PendingRegistrar]())

// TestComposingASignupDoorWithNobodyToRegisterThroughRefusesThePlan asks the two
// doors that check their own policy what happens when the registrar they write
// through is nothing. The plan answers it as the door's own refusal, naming the
// door and saying which part is missing, and nothing else is built: a door that
// cannot be filled fails its build the way every other refused build fails, with
// no panic escaping to the process that composed it.
func TestComposingASignupDoorWithNobodyToRegisterThroughRefusesThePlan(t *testing.T) {
	for _, tc := range []struct {
		door       *pkit.Module
		supplies   *pkit.Module
		name, want string
	}{
		{auth.EmailRegistration, noEmailRegistrar, "emailregistration", "email registration requires a registrar"},
		{auth.ApprovalRegistration, noPendingRegistrar, "approvalregistration", "approval registration requires a registrar"},
	} {
		_, err := pkit.NewApp("doors").Use(tc.supplies, tc.door).Plan(pkit.Deployment{Environment: pkit.Development})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s with no registrar answered %v, want a refusal saying %q", tc.name, err, tc.want)
			continue
		}
		if !strings.Contains(err.Error(), tc.name) {
			t.Errorf("%s with no registrar refused as %q, want the refusal to name the door", tc.name, err)
		}
	}
}
