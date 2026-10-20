package rest_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestADefaultedCommandAcceptsTheBodiesAtItsSchemaEdges sends bodies at the edge
// of each constraint to a command whose arguments declare defaults and to one
// whose arguments do not: both run their service once with what was sent, and a
// field the body omits reaches the defaulted service as its declared default.
func TestADefaultedCommandAcceptsTheBodiesAtItsSchemaEdges(t *testing.T) {
	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	var defaulted []constrainedDefaults
	var plain []constrainedPlain
	_, router, _ := mountAs(t, s, caller{}, func(api *httpx.API) {
		rest.Command(api.Surfaces(s.Module), s, "publish", "Publish", "", nil,
			func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in constrainedDefaults) (*Task, error) {
				defaulted = append(defaulted, in)
				return &Task{Title: "x"}, nil
			}, rest.CommandOptions{})
		rest.Command(api.Surfaces(s.Module), s, "archive", "Archive", "", nil,
			func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in constrainedPlain) (*Task, error) {
				plain = append(plain, in)
				return &Task{Title: "x"}, nil
			}, rest.CommandOptions{})
	})
	for _, tc := range []struct {
		body          string
		wantDefaulted constrainedDefaults
		wantPlain     constrainedPlain
	}{
		{`{"state":"done","note":"abc","count":5}`, constrainedDefaults{"done", "abc", 5}, constrainedPlain{"done", "abc", 5}},
		{`{"count":1}`, constrainedDefaults{"open", "ok", 1}, constrainedPlain{"", "", 1}},
		{`{"note":""}`, constrainedDefaults{"open", "", 1}, constrainedPlain{}},
		{``, constrainedDefaults{"open", "ok", 1}, constrainedPlain{}},
	} {
		t.Run(tc.body, func(t *testing.T) {
			for _, verb := range []string{"publish", "archive"} {
				before := len(defaulted) + len(plain)
				if code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task/"+uuid.NewString()+"/"+verb, tc.body); code != http.StatusOK {
					t.Fatalf("%s refused %q: %d %s", verb, tc.body, code, out)
				}
				if after := len(defaulted) + len(plain); after != before+1 {
					t.Fatalf("%s ran its service %d times for %q, want once", verb, after-before, tc.body)
				}
			}
			if got := defaulted[len(defaulted)-1]; got != tc.wantDefaulted {
				t.Errorf("defaulted service received %+v for %q, want %+v", got, tc.body, tc.wantDefaulted)
			}
			if got := plain[len(plain)-1]; got != tc.wantPlain {
				t.Errorf("plain service received %+v for %q, want %+v", got, tc.body, tc.wantPlain)
			}
		})
	}
}
