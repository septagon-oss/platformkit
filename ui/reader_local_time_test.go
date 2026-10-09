package ui

// The reader's own time is said by the browser, not by the server: components.js
// finds every <time datetime> and re-says it — relative under a week, a date for
// anything older or still to come, the exact local moment in the title. The Go
// tests of ui/resource prove the server's half (the element, its datetime, its UTC
// title); this case runs the shipped script itself, so the browser's half is proved
// by the bytes a page actually loads and not by a reading of them.

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// readerTimeHarness is the least DOM components.js needs to run its init once: a
// document that is already loaded, one <time> element, and every other query
// answering nothing. It prints the element's text and title after init.
const readerTimeHarness = `
const fs = require('fs');
const at = new Date(Date.now() + Number(process.argv[3]) * 60000).toISOString();
const el = { textContent: 'SERVER TEXT', title: 'SERVER TITLE',
  getAttribute: name => name === 'datetime' ? at : null };
globalThis.document = {
  readyState: 'complete',
  documentElement: { closest: () => ({ lang: 'en' }) },
  addEventListener() {},
  getElementById() { return null; },
  querySelectorAll: selector => selector === 'time[datetime]' ? [el] : [],
};
globalThis.window = globalThis;
new Function(fs.readFileSync(process.argv[2], 'utf8'))();
console.log(JSON.stringify({ text: el.textContent, title: el.title }));
`

func TestTheReaderSeesAnInstantInTheirOwnWords(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed; the browser's half of an instant cannot run here")
	}
	harness := t.TempDir() + "/harness.js"
	if err := os.WriteFile(harness, []byte(readerTimeHarness), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		minutes string
		want    func(text string) bool
		says    string
	}{
		{"six minutes ago reads as words", "-6", func(s string) bool { return s == "6 minutes ago" }, `"6 minutes ago"`},
		{"a time still to come reads as a date, never counted down", "120",
			func(s string) bool {
				return s != "SERVER TEXT" && !strings.HasPrefix(s, "in ") && strings.Contains(s, time.Now().Add(2*time.Hour).Format("2006"))
			},
			"an absolute local date"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command(node, harness, "assets/js/components.js", c.minutes)
			cmd.Env = append(os.Environ(), "TZ=Europe/Lisbon")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("components.js did not run: %v\n%s", err, out)
			}
			got := string(out)
			text := between(got, `"text":"`, `"`)
			title := between(got, `"title":"`, `"`)
			if !c.want(text) {
				t.Errorf("the <time> says %q; the reader should read %s (the server's words are only the fallback for a browser without Intl)", text, c.says)
			}
			if title == "SERVER TITLE" || title == "" {
				t.Errorf("the <time> title is %q; it should be the exact moment in the reader's own zone", title)
			}
		})
	}
}

func between(s, open, close string) string {
	_, rest, ok := strings.Cut(s, open)
	if !ok {
		return ""
	}
	v, _, _ := strings.Cut(rest, close)
	return v
}
