package main

import (
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
	"github.com/septagon-oss/platformkit/ui/legible"
)

// A generated form that refuses what was sent re-renders itself with the reason
// beside the field. That reason is a word a person reads, and it is read on no GET
// document: the new-task form the gate measures has no refusal on it. So either the
// gate measures the refused form, or the refused form says nothing the measured
// form did not already say outside a catalogue.
func TestARefusedFormSaysNothingTheGateDidNotMeasure(t *testing.T) {
	floor := readFloor(t)
	if _, ok := floor.Pages["POST /app/task/tasks"]; ok {
		return
	}
	path, cfg := configure(t)
	install(t, path)
	installed := catalogues()
	c := composeCopy(cfg, installed, pseudo.Wrap(installed, nil))
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	_, _, measured := get(t, cfg, admin, "/app/task/tasks/new")
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://"+cfg.Server.Addr+"/app/task/tasks",
		strings.NewReader("title=&priority=high"))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = acmeHost
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	res, err := admin.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer res.Body.Close()
	refused, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("an empty title answered %d, not the re-rendered form: %s", res.StatusCode, refused)
	}

	unwrapped := func(body []byte) []string {
		collected, err := legible.Scan(body, pseudo.Wrapped)
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		var out []string
		for _, s := range legible.Violations(collected) {
			out = append(out, strings.Join(strings.Fields(s.Text), " "))
		}
		return out
	}
	known := unwrapped(measured)
	for _, text := range unwrapped(refused) {
		if !slices.Contains(known, text) {
			t.Errorf("the refused form says %q outside a catalogue, and no document the gate measures says it", text)
		}
	}
}
