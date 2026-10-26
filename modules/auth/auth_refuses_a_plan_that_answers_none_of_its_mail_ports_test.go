package auth_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/pkit"
)

// TestAuthRefusesAPlanThatAnswersNoneOfItsMailPorts pins the arrangement the
// account-email doors cannot check for themselves: a composition that builds
// auth with no carrier, no answer about whether that carrier delivers, and no
// host to build a mailed link's address on. Each door asks those ports and
// answers with the unavailable refusal when one is missing, so a plan that
// filled them with nothing would boot a server that shows the sign-up form and
// refuses every request made to it. The four mail reads are Needed rather than
// Optional, so the plan answers for them before anything is opened.
func TestAuthRefusesAPlanThatAnswersNoneOfItsMailPorts(t *testing.T) {
	_, err := pkit.NewApp("accountmail").Use(auth.Module).Plan(pkit.Deployment{Environment: pkit.Development})
	if err == nil {
		t.Fatal("the plan built auth with no module answering any of its ports")
	}
	for _, need := range []string{
		"auth needs notificationcontracts.Mailer",
		"auth needs notificationcontracts.MailDelivery",
		"auth needs notificationcontracts.HostLookup",
		"auth needs notificationcontracts.MailLedger",
	} {
		if !strings.Contains(err.Error(), need) {
			t.Errorf("the plan refused without naming %q: %v", need, err)
		}
	}
}
