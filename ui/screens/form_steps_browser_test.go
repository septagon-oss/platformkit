package screens_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/ui"
)

func TestSteppedFormBrowserJourneys(t *testing.T) {
	tools := os.Getenv("PLATFORMKIT_BROWSER_TOOLS")
	if tools == "" {
		t.Skip("set PLATFORMKIT_BROWSER_TOOLS to an external directory containing @playwright/test")
	}
	h := browserSteps(t)
	server := httptest.NewServer(h)
	defer server.Close()
	address := strings.Replace(server.URL, "127.0.0.1", formHost, 1)
	work, err := os.MkdirTemp(tools, "journey-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if !t.Failed() {
			_ = os.RemoveAll(work)
		} else {
			t.Log("browser artifacts:", work)
		}
	})
	source, err := os.ReadFile("../../e2e/formsteps/form-sections.spec.ts")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "form-sections.spec.ts"), source, 0600); err != nil {
		t.Fatal(err)
	}
	config := fmt.Sprintf(`module.exports = { testDir: %q, outputDir: %q, workers: 1, timeout: 30000, use: {baseURL: %q, launchOptions: {executablePath: '/usr/bin/chromium', args: ['--no-sandbox', '--disable-dev-shm-usage']}} };`, work, filepath.Join(work, "results"), address)
	configPath := filepath.Join(work, "playwright.config.cjs")
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), "node", filepath.Join(tools, "node_modules/@playwright/test/cli.js"), "test", "--config", configPath)
	out, err := cmd.CombinedOutput()
	t.Log(string(out))
	if err != nil {
		t.Fatalf("browser journeys: %v", err)
	}
	if gate := os.Getenv("PLATFORMKIT_DESIGN_GATE"); gate != "" {
		for _, name := range []string{"first", "description", "options", "step-error", "final-error", "query-input", "query-result", "restart"} {
			cmd := exec.CommandContext(t.Context(), "python3", gate, address+"/snapshots/"+name, "--widths", "390,1440", "--report")
			out, err := cmd.CombinedOutput()
			t.Logf("python3 %s %s/snapshots/%s --widths 390,1440 --report\n%s", gate, address, name, out)
			if err != nil {
				t.Errorf("design %s: %v", name, err)
			}
		}
	}
}

func browserSteps(t *testing.T) http.Handler {
	t.Helper()
	r := steppedNote()
	var mutex sync.Mutex
	var saved map[string]any
	r.List = func(context.Context, crud.Query) ([]map[string]any, int64, error) { return nil, 0, nil }
	r.Create = func(_ context.Context, values map[string]any) (map[string]any, error) {
		if values["title"] == "reserved" {
			return nil, &problem.Problem{Status: 422, Detail: "Choose another title", Violations: []fault.Violation{{Location: "title", Message: "Already used"}}}
		}
		mutex.Lock()
		defer mutex.Unlock()
		values["id"] = uuid.NewString()
		saved = values
		return values, nil
	}
	r.Get = func(context.Context, uuid.UUID) (map[string]any, error) {
		mutex.Lock()
		defer mutex.Unlock()
		return saved, nil
	}
	router := formResourceServer(t, r)
	snapshots := stepSnapshots(t, router, r.Screen)
	query, _, q := readStepServer(t, func(_ context.Context, _ db.Tx[db.Tenant], _ uuid.UUID, in readQuestions) (readQuestions, error) {
		return in, nil
	})
	first := stepRequest(query, q.Screen, nil)
	state := continuation(t, first, 200, 0)
	state = continuation(t, postStep(query, q.Screen+"/steps", state, "next", url.Values{"title": {"Notebook"}}), 200, 1)
	state = continuation(t, postStep(query, q.Screen+"/steps", state, "next", url.Values{"rank": {"9007199254740993"}}), 200, 2)
	snapshots["query-input"], snapshots["query-result"] = first, postStep(query, q.Screen, state, "submit", nil)
	assets := http.StripPrefix("/admin/assets/", http.FileServerFS(ui.Assets(ui.Compose(design.Default()))))
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/admin/assets/") {
			assets.ServeHTTP(w, req)
			return
		}
		if response := snapshots[strings.TrimPrefix(req.URL.Path, "/snapshots/")]; response != nil {
			for name, values := range response.Header() {
				w.Header()[name] = values
			}
			_, _ = w.Write(response.Body.Bytes())
			return
		}
		router.ServeHTTP(w, req)
	})
}

func stepSnapshots(t *testing.T, h http.Handler, at string) map[string]*httptest.ResponseRecorder {
	t.Helper()
	first := stepRequest(h, at+"/new", nil)
	state := continuation(t, first, 200, 0)
	stale := state
	stale.Schema = "changed"
	restart := postStep(h, at+"/new/steps", stale, "next", url.Values{"title": {"private-answer-canary"}})
	description := postStep(h, at+"/new/steps", state, "next", url.Values{"title": {"reserved"}})
	state = continuation(t, description, 200, 1)
	bad := postStep(h, at+"/new/steps", state, "next", url.Values{"rank": {"bad-number"}})
	options := postStep(h, at+"/new/steps", state, "next", url.Values{"body": {"draft"}, "rank": {"7"}})
	state = continuation(t, options, 200, 2)
	final := postStep(h, at, state, "submit", nil)
	return map[string]*httptest.ResponseRecorder{"first": first, "description": description, "options": options, "step-error": bad, "final-error": final, "restart": restart}
}
