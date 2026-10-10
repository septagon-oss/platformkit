package main

// Journey 2 of the brief, end to end at the application: the source moves, the
// translation is withdrawn everywhere at once — the badge, the public text and
// the alternates — and translating the field again against the moved source
// restores all three. The delivery's load-bearing claim is that one read,
// readRecordText, answers the record screen and the public page alike, so the
// three surfaces can never disagree about which row still matches its source.
// Every assertion reaches its surface through what the correct behaviour prints
// (the record's own title, the document's language, the new English text), never
// through what a broken one would.

import (
	"net/http"
	"strings"
	"testing"
)

func TestAMovedSourceWithdrawsItsTranslationUntilItIsTranslatedAgain(t *testing.T) {
	cfg, who, id := publishedTranslationPage(t,
		map[string]string{"title": "A nossa história", "body": "O parágrafo português."})

	status, screen := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK || !strings.Contains(screen, "Our story") {
		t.Fatalf("record screen = %d", status)
	}
	if !strings.Contains(screen, "100%") {
		t.Fatalf("precondition: a fully translated record reads 100%% (the adopted completeness test pins this)")
	}

	// One English field is edited and saved; the page stays published.
	code, body := do(t, cfg, who, http.MethodPatch, acmeHost, contentPath+"/"+id,
		`{"body":"A different English paragraph."}`)
	if code != http.StatusOK {
		t.Fatalf("PATCH the source = %d %s", code, body)
	}

	status, screen = do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK || !strings.Contains(screen, "Our story") {
		t.Fatalf("record screen after the source moved = %d", status)
	}
	if strings.Contains(screen, "100%") {
		t.Error("the badge still reads 100% after the body's source moved under its translation")
	}
	if !strings.Contains(screen, "50%") {
		t.Error("one reviewed field of two reads 50%; the moved field must stop counting")
	}

	status, page := getLanguage(t, cfg, acmeHost, "/translated-story?lang=pt-PT", "pt-PT")
	if status != http.StatusOK {
		t.Fatalf("public read after the source moved = %d", status)
	}
	if !strings.Contains(page, "A nossa história") {
		t.Error("the title's translation did not move and must still be served")
	}
	if !strings.Contains(page, "A different English paragraph.") {
		t.Error("the moved body must fall back to the new English source")
	}
	if strings.Contains(page, "O parágrafo português.") {
		t.Error("a translation whose source moved is served to a public reader")
	}
	if !strings.Contains(page, `<html lang="pt-PT"`) {
		t.Error("the document is the language its reader asked for and partly got")
	}
	if strings.Contains(page, `hreflang="pt-PT"`) {
		t.Error("a record no longer whole in pt-PT is still advertised as its alternate")
	}

	// Translating the moved field against the current source restores all three
	// surfaces. The first save left the row at revision 1, and marking it
	// outdated is not an edit of the translation, so 1 is still the revision.
	code, body = do(t, cfg, who, http.MethodPost, acmeHost, contentPath+"/"+id+"/translate",
		`{"lang":"pt-PT","values":{"body":"O parágrafo novo."},"expected":{"body":1}}`)
	if code != http.StatusOK {
		t.Fatalf("translate the moved field = %d %s", code, body)
	}

	status, screen = do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	if status != http.StatusOK || !strings.Contains(screen, "100%") {
		t.Errorf("a re-translated record must read 100%% again (status %d)", status)
	}
	status, page = getLanguage(t, cfg, acmeHost, "/translated-story?lang=pt-PT", "pt-PT")
	if status != http.StatusOK || !strings.Contains(page, "O parágrafo novo.") {
		t.Errorf("the new translation must be served (status %d)", status)
	}
	if !strings.Contains(page, `hreflang="pt-PT"`) {
		t.Error("a record whole in pt-PT again must list the alternate again")
	}
}
