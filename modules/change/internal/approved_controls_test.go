package internal

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
)

func TestAnApprovedProposalOffersTheApplyControl(t *testing.T) {
	row := &contracts.Proposal{State: contracts.StateApproved, Revision: 2}
	row.ID = uuid.New()
	var page strings.Builder
	if err := decisions("/app/change/proposals", row, true).Render(&page); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.String(), `action="/app/change/proposals/`+row.ID.String()+`/apply"`) {
		t.Errorf("approved proposal has no Apply form: %s", page.String())
	}
	if strings.Contains(page.String(), `action="/app/change/proposals/`+row.ID.String()+`/review"`) {
		t.Error("approved proposal still offers a verdict that cannot change")
	}
}
