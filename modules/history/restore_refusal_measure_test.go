package history_test

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

// widestParagraph is the longest line, in characters, any visible paragraph of body
// can hold at 1440 px with the shared stylesheet: its box width over half its font
// size, the measure a reader's eye travels.
func widestParagraph(t *testing.T, body string) int {
	t.Helper()
	browser, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("Chromium is required to measure a rendered page")
	}
	script := `<script>addEventListener('load',()=>{
const vis=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>40&&r.height>6&&s.visibility!=='hidden'&&s.display!=='none'};
const m=[...document.querySelectorAll('p')].filter(vis).map(e=>Math.round(e.getBoundingClientRect().width/(parseFloat(getComputedStyle(e).fontSize)*0.5)));
document.body.dataset.widest=String(Math.max(0,...m));
});</script></body>`
	markup := strings.Replace(body, "</body>", script, 1)
	sheet := ui.Compose(design.Default())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/app.css") {
			w.Header().Set("Content-Type", "text/css")
			_, _ = w.Write(sheet.Body)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, markup)
	}))
	t.Cleanup(server.Close)
	cmd := exec.CommandContext(t.Context(), "timeout", "45", browser,
		"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run", "--disable-dev-shm-usage",
		"--window-size=1440,900", "--dump-dom", server.URL)
	cmd.Env = append(os.Environ(), "TMPDIR=/tmp")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("render the page in Chromium: %v", err)
	}
	match := regexp.MustCompile(`data-widest="([^"]+)"`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("Chromium did not measure the page")
	}
	n, err := strconv.Atoi(html.UnescapeString(string(match[1])))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A refusal is one sentence a person reads in full, so its line stays within the
// 75-character measure the rest of the workspace keeps at the widest desktop.
func TestARestoreRefusalKeepsItsLinesWithinTheReadingMeasure(t *testing.T) {
	_, router, created, screen := contentHistoryFixture(t, true)
	rec := contentRestoreForm(t, router, created, screen, "version=0&expectedRevision=2", "en")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid restore = %d %s, want 422", rec.Code, rec.Body.String())
	}
	if widest := widestParagraph(t, rec.Body.String()); widest > 75 {
		t.Errorf("the restore refusal sets a paragraph %d characters wide at 1440 px, want at most 75", widest)
	}
}
