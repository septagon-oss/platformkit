package main

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

	"github.com/septagon-oss/platformkit/modules/notification"
)

func TestARefusedProposalKeepsTwoTextAlignments(t *testing.T) {
	browser, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("Chromium is required to measure the rendered refusal page")
	}
	cfg, c, _, _ := taskChangeFixture(t, true)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	reviewer := decider(t, cfg, admin, c.mail.(*notification.Mailbox), "layout-reader")
	id := newTask(t, cfg, admin, "Inspect a valve", "normal")
	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, proposalsPath,
		`{"subjectModule":"task","subjectEntity":"task","subjectId":"`+id+
			`","diff":{"priority":"high"},"summary":"Inspect sooner"}`)
	if code != http.StatusOK && code != http.StatusCreated {
		t.Fatalf("propose = %d %s", code, body)
	}
	pid := field(t, body, "id")
	code, body = decideAsPage(t, cfg, reviewer, pid, "en", "verdict=approved&expectedRevision=1")
	if code != http.StatusSeeOther {
		t.Fatalf("approve = %d %s", code, body)
	}
	code, body = decideAsPage(t, cfg, reviewer, pid, "pt-PT", "verdict=declined&expectedRevision=1")
	if code != http.StatusConflict || !strings.Contains(body, `lang="pt-PT"`) || !strings.Contains(body, `role="alert"`) {
		t.Fatalf("did not reach the Portuguese refusal page: %d", code)
	}
	// Measure only this proposal page, using the design floor's visible block-text
	// rule: one content alignment and at most one additional rail, clustered to 3px.
	measurement := `<script>addEventListener('load',()=>{
const xs=[...document.querySelectorAll('h1,h2,h3,p')].filter(e=>{
const r=e.getBoundingClientRect(),s=getComputedStyle(e);
return r.width>40&&r.height>6&&s.visibility!=='hidden'&&s.display!=='none';
}).map(e=>Math.round(e.getBoundingClientRect().left)).sort((a,b)=>a-b);
const edges=[];for(const x of xs){if(!edges.length||x-edges.at(-1)>3)edges.push(x)}
document.body.dataset.alignments=JSON.stringify({width:innerWidth,edges});
});</script></body>`
	markup := strings.Replace(body, "</body>", measurement, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/refusal" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, markup)
			return
		}
		// Styles and other assets remain the application's own served bytes.
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet,
			"http://"+cfg.Server.Addr+r.URL.RequestURI(), nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		req.Host = acmeHost
		res, err := reviewer.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer res.Body.Close()
		w.Header().Set("Content-Type", res.Header.Get("Content-Type"))
		w.WriteHeader(res.StatusCode)
		_, _ = io.Copy(w, res.Body)
	}))
	defer server.Close()
	cmd := exec.CommandContext(t.Context(), "timeout", "45", browser,
		"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--disable-dev-shm-usage", "--user-data-dir="+t.TempDir(),
		"--window-size=1440,900", "--dump-dom", server.URL+"/refusal")
	// Chromium's singleton socket cannot fit the long task TMPDIR path. Its own
	// randomly named temporary profile is removed when this process exits.
	cmd.Env = append(os.Environ(), "TMPDIR=/tmp")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("render refusal in Chromium: %v", err)
	}
	match := regexp.MustCompile(`data-alignments="([^"]+)"`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("browser did not measure the rendered refusal")
	}
	var measured struct {
		Width int   `json:"width"`
		Edges []int `json:"edges"`
	}
	if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &measured); err != nil {
		t.Fatal(err)
	}
	if measured.Width != 1440 || len(measured.Edges) == 0 {
		t.Fatalf("unexpected measurement: %+v", measured)
	}
	if len(measured.Edges) > 2 {
		t.Errorf("refused proposal has %d text alignments at 1440px: %v; want at most two", len(measured.Edges), measured.Edges)
	}
}
