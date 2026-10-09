package main

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAProposalRefusalUsesTheRequestedPortuguese(t *testing.T) {
	cfg, _, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	id := newTask(t, cfg, admin, "Inspect a valve", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`","diff":{"priority":"high"},"summary":"Inspect sooner"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/app/change/proposals/"+pid+"/review",
		strings.NewReader("verdict=approved&expectedRevision=1"))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	page, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	// The status proves the self-review refusal was reached independently of
	// whatever words the refusal uses, in either language.
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("self-review = %d, want 409", res.StatusCode)
	}
	if !strings.Contains(string(page), `lang="pt-PT"`) {
		t.Errorf("Portuguese refusal has Content-Language=%q and no Portuguese page language", res.Header.Get("Content-Language"))
	}
	if strings.Contains(string(page), "the person who proposed a change cannot be the one who decides it") {
		t.Error("Portuguese request renders the English self-review refusal")
	}
}
