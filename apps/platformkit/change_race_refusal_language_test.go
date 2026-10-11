package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// Two reviewers open one proposal; the first decides, and the second, whose page
// is in Portuguese, posts the verdict they drew against the revision they saw. The
// refusal that answers them is the one a review page exists to give, and it is
// given in the language the page was asked in.
func TestASecondReviewersRefusalSpeaksTheRequestedPortuguese(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	first := decider(t, cfg, admin, box, "first-reviewer")
	second := decider(t, cfg, admin, box, "second-reviewer")
	id := newTask(t, cfg, admin, "Inspect a valve", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+`","diff":{"priority":"high"},"summary":"Inspect sooner"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")
	if code, body := decideAsPage(t, cfg, first, pid, "en", "verdict=approved&expectedRevision=1"); code != http.StatusOK && code != http.StatusSeeOther {
		t.Fatalf("the first verdict = %d %s", code, body)
	}

	code, page := decideAsPage(t, cfg, second, pid, "pt-PT", "verdict=declined&expectedRevision=1")
	// The status and the page language prove the refusal was reached and the page
	// was answered in Portuguese, whatever words the refusal itself uses.
	if code != http.StatusConflict {
		t.Fatalf("a second verdict on revision 1 = %d, want 409: %s", code, page)
	}
	if !strings.Contains(page, `lang="pt-PT"`) {
		t.Fatalf("the refusal page is not answered in pt-PT: %s", page)
	}
	for _, english := range []string{"this proposal is at revision", "this proposal was already", "That could not be done"} {
		if strings.Contains(page, english) {
			t.Errorf("a Portuguese reviewer is refused in English (%q)", english)
		}
	}
}

// decideAsPage posts the review page's own form, as a browser would, in one language.
func decideAsPage(t *testing.T, cfg config.Config, client *http.Client, pid, lang, form string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/app/change/proposals/"+pid+"/review", strings.NewReader(form))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = acmeHost
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", lang)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := noFollow.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	page, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(page)
}
