package main

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/modules/notification"
)

// TestEveryRefusalTheReviewPageAnswersSpeaksTheRequestedLanguage is the whole set,
// not one case from it. The review page exists to refuse people: two deciders who
// both clicked, a verdict posted once too often, the Withdraw the page itself drew,
// an apply of a row nobody approved, a proposer in front of their own proposal. Each
// of those refusals belongs to this module, so each is answered in the language the
// page was asked in, from modules/change/messages/pt-PT.json. A refusal that loses its
// key answers a Portuguese reviewer in English, and one case per sentinel is how the
// next one would be left out: this case names every refusal the doors draw.
//
// The English line in each case is the sentence the service wrote before its refusal
// was named, and the Portuguese line is the copy the catalogue answers with — the
// status proves the refusal was reached, the page language proves it was answered in
// Portuguese, and the two sentences prove which words were used for whom.
func TestEveryRefusalTheReviewPageAnswersSpeaksTheRequestedLanguage(t *testing.T) {
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	box, ok := c.mail.(*notification.Mailbox)
	if !ok {
		t.Fatalf("the composition wired %T as its mailer, want the mailbox", c.mail)
	}
	first := decider(t, cfg, admin, box, "copy-first")
	second := decider(t, cfg, admin, box, "copy-second")

	// proposed makes one task and one proposal against it, and answers the proposal's
	// id. Its own row per case, because two proposals of the same diff over one row are
	// one proposal — the unique index over an open row — and a case that silently
	// reuses another case's row refuses a state it never set up.
	proposed := func(t *testing.T, priority string) string {
		t.Helper()
		id := newTask(t, cfg, admin, "Test the relief valve", priority)
		code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
			`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+
				`","diff":{"priority":"`+priority+`"},"summary":"Move the priority to `+priority+`"}`)
		if code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("propose %s = %d %s", priority, code, body)
		}
		return field(t, body, "id")
	}
	// decided writes the verdict a case needs to reach the refusal behind it.
	decided := func(t *testing.T, who *http.Client, pid, verdict, revision string) {
		t.Helper()
		code, page := refused(t, cfg, who, pid, "review", "en",
			pageForm("verdict", verdict, "expectedRevision", revision))
		if code >= http.StatusBadRequest {
			t.Fatalf("%s of %s by the first decider = %d, want the verdict written: %s", verdict, pid, code, page)
		}
	}

	t.Run("a verdict that is not a verdict", func(t *testing.T) {
		pid := proposed(t, "high")
		refusedInPortuguese(t, cfg, first, pid, "review",
			pageForm("verdict", "maybe", "expectedRevision", "1"),
			"a verdict is approved or declined",
			"um veredicto é aprovado ou recusado")
	})

	t.Run("a verdict on a row somebody already decided", func(t *testing.T) {
		pid := proposed(t, "low")
		decided(t, first, pid, "approved", "1")
		refusedInPortuguese(t, cfg, second, pid, "review",
			pageForm("verdict", "declined", "expectedRevision", "2"),
			"this proposal was already approved",
			"esta proposta já foi decidida; se discorda do resultado, proponha a alteração de novo")
	})

	t.Run("a verdict drawn against a revision that has passed", func(t *testing.T) {
		pid := proposed(t, "critical")
		decided(t, first, pid, "approved", "1")
		refusedInPortuguese(t, cfg, second, pid, "review",
			pageForm("verdict", "declined", "expectedRevision", "1"),
			"this proposal is at revision 2",
			"esta proposta mudou desde que este formulário foi desenhado; volte a lê-la antes de decidir outra vez")
	})

	t.Run("an apply of a row nobody approved", func(t *testing.T) {
		pid := proposed(t, "normal")
		decided(t, first, pid, "declined", "1")
		refusedInPortuguese(t, cfg, second, pid, "apply",
			pageForm("expectedRevision", "2"),
			"only an approved proposal may be applied, and this one is declined",
			"só uma proposta aprovada pode ser aplicada; esta proposta não está aprovada")
	})

	t.Run("a withdraw by a person who did not propose", func(t *testing.T) {
		pid := proposed(t, "high")
		refusedInPortuguese(t, cfg, second, pid, "withdraw",
			pageForm("expectedRevision", "1"),
			"only the proposer may withdraw a proposal",
			"só a pessoa que propôs uma alteração a pode retirar; se discorda dela, recuse-a")
	})

	t.Run("a withdraw of a proposal that is over", func(t *testing.T) {
		pid := proposed(t, "low")
		decided(t, first, pid, "declined", "1")
		refusedInPortuguese(t, cfg, admin, pid, "withdraw",
			pageForm("expectedRevision", "2"),
			"a declined proposal is over",
			"esta proposta já terminou e não aceita mais comandos; proponha a alteração de novo")
	})

	t.Run("a proposer deciding their own proposal", func(t *testing.T) {
		pid := proposed(t, "critical")
		refusedInPortuguese(t, cfg, admin, pid, "review",
			pageForm("verdict", "approved", "expectedRevision", "1"),
			"the person who proposed a change cannot be the one who decides it",
			"a pessoa que propôs uma alteração não pode ser a que decide sobre ela")
	})

	t.Run("an apply of a subject that moved underneath it", func(t *testing.T) {
		pid := proposed(t, "normal")
		task := subject(t, cfg, admin, pid)
		decided(t, first, pid, "approved", "1")
		// The task itself, through the one door that stays open while the switch is
		// on: a title is nobody's protected field, and the write moves the revision
		// the approved diff was measured against, which is all a stale base is.
		if code, body := do(t, cfg, admin, http.MethodPatch, acmeHost, tasksPath+"/"+task,
			`{"title":"Test the relief valve again"}`); code != http.StatusOK {
			t.Fatalf("the direct write = %d %s", code, body)
		}
		refusedInPortuguese(t, cfg, second, pid, "apply",
			pageForm("expectedRevision", "2"),
			"it was made against revision",
			"o registo mudou desde que esta alteração foi preparada; faça uma nova proposta contra a revisão atual")
	})
}

// refused posts one of the review page's own forms, in one language, and answers the
// status and the page. No redirect is followed: the person who was refused reads this
// page, and it is only the command that succeeded that gets a SeeOther.
func refused(t *testing.T, cfg config.Config, client *http.Client, pid, door, lang, form string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost,
		"http://"+cfg.Server.Addr+"/app/change/proposals/"+pid+"/"+door, strings.NewReader(form))
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

// refusedInPortuguese is one case: post the form in Portuguese, then say what the
// refusal is supposed to have done about the language.
func refusedInPortuguese(t *testing.T, cfg config.Config, client *http.Client, pid, door, form, english, portuguese string) {
	t.Helper()
	code, page := refused(t, cfg, client, pid, door, "pt-PT", form)
	// The status says a refusal was reached, whichever words it is wearing. The page
	// answers every command refusal with the status of a conflict; anything less than
	// a refusal here means the case proved nothing about the copy.
	if code < http.StatusBadRequest {
		t.Fatalf("%s of %s = %d, want a refusal: %s", door, pid, code, pageHead(page))
	}
	if !strings.Contains(page, `lang="pt-PT"`) {
		t.Fatalf("the page was not answered in pt-PT: %s", pageHead(page))
	}
	if strings.Contains(page, english) {
		t.Errorf("a Portuguese reviewer is refused in English (%q)", english)
	}
	if strings.Contains(page, "That could not be done") {
		t.Errorf("the alert's own heading stayed English on a Portuguese page")
	}
	if !strings.Contains(page, portuguese) {
		t.Errorf("the refusal never says %q: %s", portuguese, pageHead(page))
	}
}

// pageForm is the form a browser would post: exactly the fields the page's own control
// carries, none of them invented for the test.
func pageForm(pairs ...string) string {
	form := url.Values{}
	for at := 0; at+1 < len(pairs); at += 2 {
		form.Set(pairs[at], pairs[at+1])
	}
	return form.Encode()
}

// subject is the task one proposal was made against, read from the row's own subject
// id, which is the line the page prints as its subject.
func subject(t *testing.T, cfg config.Config, admin *http.Client, pid string) string {
	t.Helper()
	code, body := do(t, cfg, admin, http.MethodGet, acmeHost, proposalsPath+"/"+pid, "")
	if code != http.StatusOK {
		t.Fatalf("read the proposal = %d %s", code, body)
	}
	return field(t, body, "subjectId")
}

// pageHead is what a failure prints: the front of the page, enough to see what was drawn
// without pouring a whole document into the log.
func pageHead(page string) string {
	if len(page) > 900 {
		return page[:900] + "…"
	}
	return page
}
