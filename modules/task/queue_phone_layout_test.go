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

func TestTaskQueueKeepsTheOverdueResponseReadableAtPhoneWidth(t *testing.T) {
	browser, err := exec.LookPath("chromium")
	if err != nil {
		t.Skip("Chromium is required to measure the task queue layout")
	}
	queue := resource.Resource{Schema: task.Spec.Schema(), Present: task.Spec.Present,
		Immutable: task.Spec.Immutable, Screen: "/app/task/tasks"}
	rows := []map[string]any{{"id": "breached", "title": "Inspect malfunctioning ventilation",
		"priority": "critical", "status": "open", "dueAt": "2026-10-22T12:00:00Z", "slaBreached": true}}
	view := resource.List(queue, resource.Options{Workspace: "/app", Home: "Dashboard"}, rows, 1, 1, "", true)
	sheet := ui.Compose(design.Default())
	view.Body = append(view.Body, g.Raw(`<script>
addEventListener('load', () => {
  const visible = e => {
    if (!e || e.getClientRects().length === 0) return false;
    const range = document.createRange();
    range.selectNodeContents(e);
    const box = range.getBoundingClientRect();
    if (box.width === 0 || box.height === 0 || box.left < 0 || box.right > innerWidth) return false;
    for (let p = e.parentElement; p; p = p.parentElement) {
      const style = getComputedStyle(p), clip = p.getBoundingClientRect();
      if (style.display === 'none' || style.visibility === 'hidden') return false;
      if (['auto','scroll','hidden','clip'].includes(style.overflowX) &&
          (box.left < clip.left || box.right > clip.right)) return false;
    }
    return true;
  };
  const row = document.querySelector('[data-pk-row="breached"]');
  const labels = [...document.querySelectorAll('th,dt,label,span')]
    .filter(e => e.textContent.trim() === 'Response overdue');
  const values = row ? [...row.querySelectorAll('td,dd,span')].filter(e => e.textContent.trim() === 'Yes') : [];
  const header = labels[0]?.getBoundingClientRect();
  document.body.dataset.queueResult = JSON.stringify({width:innerWidth, row:!!row,
    label:labels.some(visible), value:values.some(visible), left:header?.left, right:header?.right});
});
</script>`))
	page := document.Document(document.Chrome{Assets: "/assets", Stylesheet: sheet}, document.Request{}, view,
		components.Shell(components.ShellProps{}, components.ShellSlots{Main: view.Body}))
	assets := http.StripPrefix("/assets/", http.FileServer(http.FS(ui.Assets(sheet))))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/queue" {
			w.Header().Set("Content-Type", "text/html")
			if err := page.Render(w); err != nil {
				t.Error(err)
			}
			return
		}
		if r.URL.Path == "/frame" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = fmt.Fprintf(w, `<!doctype html><iframe style="border:0;width:%spx;height:900px" src="/queue"></iframe><script>
addEventListener('load',()=>{document.body.dataset.queueResult=document.querySelector('iframe').contentDocument.body.dataset.queueResult||''});
</script>`, r.URL.Query().Get("width"))
			return
		}
		assets.ServeHTTP(w, r)
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
				t.Fatalf("render task queue in Chromium: %v", err)
			}
			match := regexp.MustCompile(`data-queue-result="([^"]+)"`).FindSubmatch(raw)
			if len(match) != 2 {
				t.Fatalf("no queue measurement in %s", strings.TrimSpace(string(raw)))
			}
			var got struct {
				Width             int
				Row, Label, Value bool
				Left, Right       float64
			}
			if err := json.Unmarshal([]byte(html.UnescapeString(string(match[1]))), &got); err != nil {
				t.Fatal(err)
			}
			if !got.Row || got.Width != width {
				t.Fatalf("queue not reached at the requested viewport: %+v", got)
			}
			if !got.Label || !got.Value {
				t.Errorf("at %dpx the response-overdue label and stored Yes must fit without horizontal scrolling: %+v", width, got)
			}
		})
	}
}
