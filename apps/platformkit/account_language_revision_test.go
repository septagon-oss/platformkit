package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheAccountLanguageCommandRejectsAStaleRevision(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	const path = "/api/v1/user/me/language"
	read := func() struct {
		Language string `json:"language"`
		Revision int64  `json:"revision"`
	} {
		t.Helper()
		code, body := do(t, cfg, admin, http.MethodGet, acmeHost, path, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", path, code, body)
		}
		var state struct {
			Language string `json:"language"`
			Revision int64  `json:"revision"`
		}
		if err := json.Unmarshal([]byte(body), &state); err != nil {
			t.Fatal(err)
		}
		return state
	}
	before := read()
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	events := func() int {
		t.Helper()
		var count int
		if err := owner.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE name = 'user.language_set'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	first := fmt.Sprintf(`{"language":"pt-PT","expectedRevision":%d}`, before.Revision)
	if code, body := do(t, cfg, admin, http.MethodPut, acmeHost, path, first); code < 200 || code >= 300 {
		t.Fatalf("first preference write = %d: %s", code, body)
	}
	acceptedEvents := events()
	stale := fmt.Sprintf(`{"language":"en","expectedRevision":%d}`, before.Revision)
	code, body := do(t, cfg, admin, http.MethodPut, acmeHost, path, stale)
	if code != http.StatusConflict || !strings.Contains(strings.ToLower(body), "revision") {
		t.Errorf("stale preference write = %d: %s, want a revision conflict", code, body)
	}
	if strings.Contains(body, `"preferredLocale"`) {
		t.Errorf("the refused write returned a user row: %s", body)
	}
	if got := events(); got != acceptedEvents {
		t.Errorf("the refused write emitted %d further events", got-acceptedEvents)
	}
	after := read()
	if after.Language != "pt-PT" || after.Revision == before.Revision {
		t.Errorf("after the stale write the preference is %+v, want pt-PT at a new revision", after)
	}
}

func TestTheLanguageDoorDoesNotAcceptTwoWritesAtOneRevision(t *testing.T) {
	cfg, admin := languageApp(t, `{"default":"en","supported":["pt-PT"]}`)
	const path = "/api/v1/user/users/language"
	first := `{"locale":"pt-PT","expectedRevision":0}`
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, path, first)
	if code == http.StatusNotFound || code == http.StatusMethodNotAllowed {
		return // The revised account command may replace this old route.
	}
	if code == http.StatusUnprocessableEntity && strings.Contains(body, "body.expectedRevision: unexpected property") {
		firstCode, firstBody := do(t, cfg, admin, http.MethodPost, acmeHost, path, `{"locale":"pt-PT"}`)
		if firstCode >= 400 {
			return // This old route refuses an unversioned write.
		}
		secondCode, secondBody := do(t, cfg, admin, http.MethodPost, acmeHost, path, `{"locale":"en"}`)
		t.Errorf("the door rejects expectedRevision but accepts two unversioned writes: first %d %s; second %d %s",
			firstCode, firstBody, secondCode, secondBody)
		return
	}
	if code != http.StatusOK {
		t.Fatalf("first preference write = %d: %s", code, body)
	}
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	var acceptedEvents int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = 'user.language_set'`).Scan(&acceptedEvents); err != nil {
		t.Fatal(err)
	}
	stale := `{"locale":"en","expectedRevision":0}`
	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, path, stale)
	if code != http.StatusConflict || !strings.Contains(strings.ToLower(body), "revision") {
		t.Errorf("second preference write at revision 0 = %d: %s, want a revision conflict", code, body)
	}
	var afterEvents int
	if err := owner.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE name = 'user.language_set'`).Scan(&afterEvents); err != nil {
		t.Fatal(err)
	}
	if afterEvents != acceptedEvents {
		t.Errorf("the stale write emitted %d events", afterEvents-acceptedEvents)
	}
	if got := pageLang(dashboard(t, cfg, admin, "en")); got != "pt-PT" {
		t.Errorf("the refused write left the page in %q, want pt-PT", got)
	}
}
