package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
)

// A served export must let its reader tell a removed option from a current one:
// either the removed option is absent from `options`, or it carries a visible
// `removedAt`. An option that is in `options` but not in `results.options` with
// no marker reads as a current option that somehow has no tally.
func TestAServedExportTellsRemovedOptionsFromCurrentOnes(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := w.svc.RemoveOption(ctx, tx, contracts.RemoveOptionInput{QuestionInput: contracts.QuestionInput{QuestionID: decidetest.Q, IfRevision: 1}, OptionID: decidetest.A})
		return err
	})
	_, handler := mountedDecide(t, w)
	resp := request(t, handler, http.MethodGet, "/api/v1/decide/questions/"+decidetest.Q.String()+"/export", "")
	if resp.Code != http.StatusOK {
		t.Fatalf("export: %d %s", resp.Code, resp.Body.String())
	}
	var export struct {
		Options []map[string]any `json:"options"`
		Results struct {
			Options []struct {
				Option struct {
					ID string `json:"id"`
				} `json:"option"`
			} `json:"options"`
		} `json:"results"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &export); err != nil {
		t.Fatal(err)
	}
	current := map[string]bool{}
	for _, row := range export.Results.Options {
		current[row.Option.ID] = true
	}
	if len(current) != 2 || current[decidetest.A.String()] {
		t.Fatalf("results should rank the two current options only: %v", current)
	}
	for _, option := range export.Options {
		id, _ := option["id"].(string)
		if current[id] {
			continue
		}
		if marker, ok := option["removedAt"]; !ok || marker == nil {
			t.Fatalf("option %s is exported without a tally and without a removedAt marker: %v", id, option)
		}
	}
}
