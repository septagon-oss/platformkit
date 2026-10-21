package task_test

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTaskQueueKeepsDueSortingAvailableOnPhones(t *testing.T) {
	browser, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("Chromium is required to check keyboard access to task sorting")
	}
	queue := resource.Resource{Schema: task.Spec.Schema(), Present: task.Spec.Present,
		Immutable: task.Spec.Immutable, Screen: "/app/task/tasks"}
	rows := []map[string]any{{"id": "overdue", "title": "Inspect ventilation",
		"priority": "critical", "status": "open", "dueAt": "2026-10-22T12:00:00Z", "slaBreached": true}}
	view := resource.List(queue, resource.Options{Workspace: "/app", Home: "Dashboard"}, rows, 1, 1, "", true)
	sheet := ui.Compose(design.Default())
	view.Body = append(view.Body, g.Raw(`<script>
addEventListener('load', () => {
  const visible = e => e.getClientRects().length > 0 &&
    getComputedStyle(e).visibility !== 'hidden' && !e.disabled;
  const candidates = [...document.querySelectorAll('a,button,select')].filter(e => {
    if (e.dataset.pkSort === 'dueAt') return true;
    if (e.tagName === 'A') return new URL(e.href).searchParams.get('sort') === 'dueAt';
    return e.tagName === 'SELECT' && [...e.options].some(o => o.value === 'dueAt');
  });
  const control = candidates.find(visible);
  if (control) control.focus();
  document.body.dataset.sortResult = JSON.stringify({width:innerWidth,
    row:!!document.querySelector('[data-pk-row="overdue"]'),
    control:!!control, focused:!!control && document.activeElement === control});
});
</script>`))
	page := document.Document(document.Chrome{Assets: "/assets", Stylesheet: sheet}, document.Request{}, view,
		components.Shell(components.ShellProps{}, components.ShellSlots{Main: view.Body}))
	assets := http.StripPrefix("/assets/", http.FileServer(http.FS(ui.Assets(sheet))))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/queue":
			w.Header().Set("Content-Type", "text/html")
			if err := page.Render(w); err != nil {
				t.Error(err)
			}
		case "/frame":
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<!doctype html><iframe style="border:0;width:%spx;height:900px" src="/queue"></iframe><script>
addEventListener('load',()=>{document.body.dataset.sortResult=document.querySelector('iframe').contentDocument.body.dataset.sortResult||''});
</script>`, r.URL.Query().Get("width"))
		default:
			assets.ServeHTTP(w, r)
		}
	}))
	defer server.Close()
	for _, width := range []int{390, 1440} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), "timeout", "45", browser, "--headless", "--no-sandbox",
				"--disable-gpu", "--disable-dev-shm-usage", "--no-first-run", "--user-data-dir="+t.TempDir(),
				"--dump-dom", fmt.Sprintf("%s/frame?width=%d", server.URL, width))
			cmd.Env = append(os.Environ(), "TMPDIR=/tmp")
			raw, err := cmd.Output()
			if err != nil {
				t.Fatalf("render the task sorting controls: %v", err)
			}
			match := regexp.MustCompile(`data-sort-result="([^"]+)"`).FindSubmatch(raw)
			if len(match) != 2 {
				t.Fatalf("no sorting measurement in %s", strings.TrimSpace(string(raw)))
			}
			var got struct {
				Width                 int
				Row, Control, Focused bool
			}
			if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &got); err != nil {
				t.Fatal(err)
			}
			if !got.Row || got.Width != width {
				t.Fatalf("queue not reached at the requested viewport: %+v", got)
			}
			if !got.Control || !got.Focused {
				t.Errorf("the queue must retain a visible, keyboard-focusable Due sort at %dpx: %+v", width, got)
			}
		})
	}
}
