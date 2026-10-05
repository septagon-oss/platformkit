package admin_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/httpx"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func TestAKeyedRevocationStillChecksTheSessionsRevision(t *testing.T) {
	store := threeSessions()
	router := mountAs(t, caller{}, withSessions(store))
	page := signedInAs(t, router, http.MethodGet, "/app/auth/sessions", "")
	if page.Code != http.StatusOK {
		t.Fatalf("sessions page = %d %s", page.Code, page.Body)
	}
	expected := hiddenField(t, page.Body.String(), "expected")
	store.items = append(store.items, &authcontracts.SessionListing{Ref: "later-session"})
	store.ids["later-session"] = uuid.New()
	req := httptest.NewRequest(http.MethodPost, "http://"+host+"/app/auth/sessions/revoke-rest",
		strings.NewReader("expected="+expected))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set(httpx.IdempotencyKeyHeader, uuid.NewString())
	req.AddCookie(&http.Cookie{Name: httpx.CookieName(httpx.SessionCookie, false), Value: here.String()})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	if response.Code != http.StatusConflict {
		t.Errorf("keyed stale-list revocation = %d %s; want 409", response.Code, response.Body)
	}
	if len(store.revoked) != 0 {
		t.Errorf("keyed stale-list revocation ended %d sessions; want none", len(store.revoked))
	}
	if len(store.items) != 4 {
		t.Errorf("keyed refusal left %d sessions; want the original four", len(store.items))
	}
}
