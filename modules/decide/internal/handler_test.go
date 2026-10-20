package internal_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

type authority struct{}

func (authority) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return tenancy.Tenant{ID: decidetest.Tenant}, nil
}
func (authority) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

func mountedDecide(t *testing.T, w *world) (*httpx.API, http.Handler) {
	t.Helper()
	api, router := httpx.New(httpx.Options{Conn: w.conn, Cache: cache.Memory("decide"), PublicHost: "decide.test", Tenants: authority{}, Authorize: authority{}, Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: decidetest.Alice}, true, nil
		}})
	w.svc.Routes(api.Surfaces("decide"))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	if bad := httpx.CheckRowHints(api.Resources()); bad != "" {
		t.Fatal(bad)
	}
	return api, router
}

func request(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://decide.test"+path, strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Requested-With", "XMLHttpRequest")
		r.Header.Set("Origin", "http://decide.test")
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func TestParticipantDeckIsServedWithBoundUndoableCommands(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
	api, handler := mountedDecide(t, w)
	deck := request(t, handler, http.MethodGet, "/api/v1/decide/questions/"+decidetest.Q.String()+"/deck", "")
	if deck.Code != http.StatusOK {
		t.Fatalf("deck: %d %s", deck.Code, deck.Body.String())
	}
	var got contracts.Deck
	if err := json.Unmarshal(deck.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 || len(got.Cards) != 3 || got.Cards[0].VoteRevision != 0 || got.Cards[0].ID != decidetest.B {
		t.Fatalf("deck: %+v", got)
	}
	catalog := screens.Describe1(api.Resources()[1], true)
	if !catalog.Presentation.Deck.Undoable || catalog.Presentation.Deck.Directions["down"].Command != "skip" {
		t.Fatalf("catalogue: %+v", catalog.Presentation)
	}
	for _, command := range catalog.Commands {
		if command.Verb == "yes" && (command.Bindings["ifRevision"] != "voteRevision" || command.Undo != "undo-vote" || !command.HasResult) {
			t.Fatalf("command: %+v", command)
		}
	}
	cast := request(t, handler, http.MethodPost, "/api/v1/decide/cards/"+decidetest.B.String()+"/yes", `{"questionId":"`+decidetest.Q.String()+`","ifRevision":0}`)
	if cast.Code != http.StatusOK {
		t.Fatalf("vote: %d %s", cast.Code, cast.Body.String())
	}
	var reply struct {
		Result    contracts.Vote `json:"result"`
		UndoToken string         `json:"undoToken"`
	}
	if err := json.Unmarshal(cast.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Result.Revision != 1 || reply.UndoToken == "" {
		t.Fatalf("reply: %+v", reply)
	}
	undo := request(t, handler, http.MethodPost, "/api/v1/decide/cards/undo-vote", `{"token":"`+reply.UndoToken+`"}`)
	if undo.Code != http.StatusOK {
		t.Fatalf("undo: %d %s", undo.Code, undo.Body.String())
	}
	queued := request(t, handler, http.MethodGet, "/api/v1/decide/cards?filter=questionId:"+decidetest.Q.String()+"&filter=unvoted:true&sort=position", "")
	if queued.Code != http.StatusOK || !strings.Contains(queued.Body.String(), `"voteRevision":2`) {
		t.Fatalf("queue after undo: %d %s", queued.Code, queued.Body.String())
	}
	stale := request(t, handler, http.MethodPost, "/api/v1/decide/cards/"+decidetest.B.String()+"/strong", `{"questionId":"`+decidetest.Q.String()+`","ifRevision":0}`)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale bound card: %d %s", stale.Code, stale.Body.String())
	}
}
