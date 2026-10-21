package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// The catalogue is the contract a generic deck client reads: a resource that
// declares a deck promises that listing it with the declared queue filters and
// order answers the caller's queue. A participant listing Decide's cards exactly
// as the catalogue says must receive the cards awaiting their vote, not a refusal.
func TestTheDeclaredDeckQueueListsTheParticipantsCards(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
	api, handler := mountedDecide(t, w)
	for _, r := range api.Resources() {
		entry := screens.Describe1(r, true)
		if entry.Presentation == nil || entry.Presentation.Deck == nil {
			continue
		}
		deck := entry.Presentation.Deck
		query := url.Values{}
		for _, filter := range deck.Queue {
			query.Add("filter", filter)
		}
		if deck.Order != "" {
			query.Set("sort", deck.Order)
		}
		path := "/api/v1/" + r.Module + r.Path + "?" + query.Encode()
		resp := request(t, handler, http.MethodGet, path, "")
		if resp.Code != http.StatusOK {
			t.Fatalf("%s/%s declares a deck whose queue cannot be listed as declared: GET %s answered %d %s", r.Module, r.Entity, path, resp.Code, resp.Body.String())
		}
		var page struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(resp.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, item := range page.Items {
			id, _ := item["id"].(string)
			ids = append(ids, id)
		}
		if !strings.Contains(strings.Join(ids, ","), decidetest.B.String()) {
			t.Fatalf("%s/%s queue for a participant who has voted on nothing omits an unvoted card: %v", r.Module, r.Entity, ids)
		}
	}
}
