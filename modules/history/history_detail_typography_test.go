package history_test

import (
	"encoding/json"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
)

func TestAVersionedPageUsesAtMostTwoBodyTextSizes(t *testing.T) {
	_, router, created, screen := contentHistoryFixture(t)
	code, body := call(t, router, acmeHost, http.MethodGet, screen+"/"+created.ID.String(), "")
	if code != http.StatusOK {
		t.Fatalf("detail = %d %s", code, body)
	}
	measured := measureHistoryPage(t, body)
	if len(measured.Sizes) > 2 {
		t.Errorf("versioned detail has %d body text sizes %v, want at most two", len(measured.Sizes), measured.Sizes)
	}
}

type historyPageLayout struct {
	Width int   `json:"width"`
	Sizes []int `json:"sizes"`
	Edges []int `json:"edges"`
}

func measureHistoryPage(t *testing.T, body string) historyPageLayout {
	t.Helper()
	browser, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("Chromium is required to measure rendered history pages")
	}
	measurement := `<script>addEventListener('load',()=>{
const visible=e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e);return r.width>40&&r.height>6&&s.visibility!=='hidden'&&s.display!=='none'};
const sizes=[...new Set([...document.querySelectorAll('p')].filter(visible).map(e=>Math.round(parseFloat(getComputedStyle(e).fontSize))))].sort((a,b)=>a-b);
const xs=[...document.querySelectorAll('h1,h2,h3,p')].filter(visible).map(e=>Math.round(e.getBoundingClientRect().left)).sort((a,b)=>a-b);
const edges=[];for(const x of xs){if(!edges.length||x-edges.at(-1)>3)edges.push(x)}
document.body.dataset.historyLayout=JSON.stringify({width:innerWidth,sizes,edges});
});</script></body>`
	markup := strings.Replace(body, "</body>", measurement, 1)
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
		t.Fatalf("render detail in Chromium: %v", err)
	}
	match := regexp.MustCompile(`data-history-layout="([^"]+)"`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("Chromium did not measure the detail")
	}
	var measured historyPageLayout
	if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &measured); err != nil {
		t.Fatal(err)
	}
	if measured.Width != 1440 || len(measured.Sizes) == 0 {
		t.Fatalf("unexpected measurement: %+v", measured)
	}
	return measured
}
