package admin_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestSelfCommandDiscoveryDoesNotRequireReadingOtherPeoplesRows(t *testing.T) {
	api, router := mountWithAPI(t, member{})
	rest.Command(api.Surfaces("note"), spec, "language", "Choose language", "The reader's own choice", nil,
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, struct{}) (*Note, error) {
			return &Note{Title: "Own choice"}, nil
		}, rest.CommandOptions{Auth: httpx.SignedIn(), Collection: true, Self: true})
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/api/v1/note/notes/language", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: "present"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("signed-in self command = %d: %s", w.Code, w.Body.String())
	}
	code, body, _ := call(t, router, http.MethodGet, "/api/v1/app/resources", "")
	if code != http.StatusOK {
		t.Fatalf("catalog = %d: %s", code, body)
	}
	var catalog screens.Catalog
	if err := json.Unmarshal([]byte(body), &catalog); err != nil {
		t.Fatal(err)
	}
	for _, resource := range catalog.Resources {
		for _, command := range resource.Commands {
			if command.Verb == "language" && command.Self {
				return
			}
		}
	}
	t.Errorf("a signed-in member can execute the self command but cannot discover it: %s", body)
}
