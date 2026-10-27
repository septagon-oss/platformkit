package internal

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

func TestDecisionAnchorsFollowProposalStateAndActorGrant(t *testing.T) {
	for _, tc := range []struct {
		state string
		may   bool
		want  []string
	}{
		{contracts.StateProposed, true, []string{"approve", "decline", "withdraw"}},
		{contracts.StateApproved, true, []string{"apply", "withdraw"}},
		{contracts.StateProposed, false, nil},
	} {
		row := &contracts.Proposal{State: tc.state, Revision: 1}
		var body strings.Builder
		if err := decisions("/app/change/proposals", row, tc.may).Render(&body); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"approve", "decline", "apply", "withdraw"} {
			want := 0
			for _, active := range tc.want {
				if active == name {
					want = 1
				}
			}
			ref := `data-screen-anchor="app/change/custom/proposal#action.` + name + `"`
			if got := strings.Count(body.String(), ref); got != want {
				t.Errorf("state %s may=%v anchor %s = %d, want %d", tc.state, tc.may, name, got, want)
			}
		}
	}
}
