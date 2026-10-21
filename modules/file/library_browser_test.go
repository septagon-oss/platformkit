package file_test

import (
	"encoding/json"
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

func libraryBrowser(t *testing.T, router http.Handler, at, script string, width int) map[string]json.RawMessage {
	t.Helper()
	browser, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("Chromium is required to check the browser form and layout")
	}
	rec := libraryRequest(t, router, http.MethodGet, at, host, "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d", at, rec.Code)
	}
	markup := strings.Replace(rec.Body.String(), "</body>", `<script>addEventListener('load',()=>{`+script+`});</script></body>`, 1)
	sheet := ui.Compose(design.Default())
	assets := http.StripPrefix("/assets/", http.FileServer(http.FS(ui.Assets(sheet))))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/library" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, markup)
			return
		}
		if r.URL.Path == "/wrap" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, `<!doctype html><iframe id="view" style="border:0;width:`+strconv.Itoa(width)+`px;height:900px" src="/library"></iframe><script>addEventListener('load',()=>{document.body.dataset.result=document.querySelector('iframe').contentDocument.body.dataset.result||''})</script>`)
			return
		}
		assets.ServeHTTP(w, r)
	}))
	defer server.Close()
	cmd := exec.CommandContext(t.Context(), "timeout", "45", browser, "--headless", "--no-sandbox", "--disable-gpu",
		"--disable-dev-shm-usage", "--no-first-run", "--user-data-dir="+t.TempDir(), "--dump-dom", server.URL+"/wrap")
	cmd.Env = append(os.Environ(), "TMPDIR=/tmp")
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("render the library in Chromium: %v", err)
	}
	match := regexp.MustCompile(`data-result="([^"]+)"`).FindSubmatch(raw)
	if len(match) != 2 {
		t.Fatal("the browser did not return a measurement")
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &result); err != nil {
		t.Fatal(err)
	}
	return result
}
