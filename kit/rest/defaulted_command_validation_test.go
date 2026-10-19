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

type constrainedDefaults struct {
	State string `json:"state" enum:"open,done" default:"open"`
	Note  string `json:"note" maxLength:"3" default:"ok"`
	Count int64  `json:"count" minimum:"1" maximum:"5" default:"1"`
}

type constrainedPlain struct {
	State string `json:"state,omitempty" enum:"open,done"`
	Note  string `json:"note,omitempty" maxLength:"3"`
	Count int64  `json:"count,omitempty" minimum:"1" maximum:"5"`
}

// TestADefaultedCommandValidatesItsBodyLikeAnUndefaultedOne sends the same
// out-of-schema bodies to a command whose arguments declare defaults and to one
// whose arguments do not: both must refuse with 422 before the service runs.
func TestADefaultedCommandValidatesItsBodyLikeAnUndefaultedOne(t *testing.T) {
	s := deckSpec()
	delete(s.Present.Deck.Directions, "left")
	var defaulted, plain []any
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
	for _, body := range []string{
		`{"state":"bogus"}`,
		`{"note":"toolong"}`,
		`{"count":0}`,
		`{"count":9}`,
		`{"count":1.5}`,
		`{"unknown":1}`,
		`[]`,
		`"text"`,
	} {
		for _, verb := range []string{"publish", "archive"} {
			t.Run(verb+" "+body, func(t *testing.T) {
				before := len(defaulted) + len(plain)
				code, out := call(t, router, http.MethodPost, "/api/v1/tasks/task/"+uuid.NewString()+"/"+verb, body)
				if code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
					t.Errorf("%s accepted %s: %d %s", verb, body, code, out)
				}
				if after := len(defaulted) + len(plain); after != before {
					t.Errorf("%s ran its service for %s", verb, body)
				}
			})
		}
	}
}
