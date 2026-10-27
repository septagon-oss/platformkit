package internal_test

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// A shut sign-in page says one sentence about every address, and that sentence is the
// whole of what a stranger learns from it. One address here has nobody behind it, one has
// a person a customer invited by mail who has set no password yet, and one has a member
// who did. All three answer the same refusal once their own allowance of wrong answers is
// spent, and the refusal arrives whichever password is then offered.

// The three belong in one case because two of the wrong-answer refusals do not come from
// the same branch: an address nobody knows and an invited person who cannot sign in are
// two separate early returns, each of which records its failure before anything reads the
// counter. A build that stopped counting for either of them would leave the lockout itself
// as the oracle — type at the page ten times and see which address shuts.
func TestALockedSignInPageShutsEveryKindOfAddressItIsGiven(t *testing.T) {
	const (
		unseen  = "nobody.behind.this@example.com"
		invited = "still.waiting.for.its.link@example.com"
		member  = "already.chose.a.password@example.com"
		chosen  = "a sufficiently strong password"
		wrong   = "not the password anybody was invited with"
	)
	f := invitationSQL(t)
	waiting := invitedIn(t, f, f.ctx, invited)
	if offeredIn(t, f, f.ctx, waiting) == "" {
		t.Fatalf("no link was mailed to %s, so no invited person was waiting there for the page to shut", invited)
	}
	settled := invitedIn(t, f, f.ctx, member)
	link := offeredIn(t, f, f.ctx, settled)
	if link == "" {
		t.Fatalf("no link was mailed to %s, so no member would be locked out below", member)
	}
	if _, err := setPasswordIn(t, f, f.ctx, link); err != nil {
		t.Fatalf("%s setting its password through the link it was mailed returned %v, want a member to lock out",
			member, err)
	}
	for _, address := range []string{unseen, invited, member} {
		for n := range contracts.MaxAttempts {
			if _, _, err := signedInAt(t, f, f.ctx, address, wrong); !errors.Is(err, contracts.ErrCredentials) {
				t.Fatalf("%s failure %d at the sign-in page returned %v, want the wrong-answer refusal it is counting",
					address, n+1, err)
			}
		}
		if _, _, err := signedInAt(t, f, f.ctx, address, chosen); !errors.Is(err, contracts.ErrTooManyAttempts) {
			t.Errorf("%s answered %v at the sign-in page after the %d failures it counted, want the one lockout every "+
				"address gets whatever this address is", address, err, contracts.MaxAttempts)
		}
	}
}
