package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/app"
)

func TestTheReferenceShellAndGeneratedPagesMeetTheDesignFloor(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, who, http.MethodPost, acmeHost, "/api/v1/task/tasks",
		`{"title":"A measured task","description":"A clear next step.","priority":"normal"}`)
	if code != http.StatusCreated {
		t.Fatalf("create the measured task: %d %s", code, body)
	}
	id := field(t, body, "id")
	// The record is measured as the person who kept a copy of it returns to it. The
	// composition draws a keep control there, so pressing it changes what the page
	// offers to do, and a record nobody kept would leave that pressed state unseen
	// and the floor never asked of it.
	record := servedPage(t, cfg, who, acmeHost, "/app/task/tasks/"+id)
	if code, _, fault := askForm(t, cfg, who, acmeHost, drawnAddress(t, record, "/saved/save"), saveForm(t, record, id)); code != http.StatusSeeOther {
		t.Fatalf("keep a copy of the measured task: %d (%s)", code, fault)
	}
	// A second copy is kept because the kept list's content is whatever the person
	// kept: with one card the page draws its own actions once, and the state every later
	// press leaves it in is never measured. Both the list of everything and the list
	// filtered to one pair are read off the record's own links, because the mount writes
	// those addresses and no test should guess at them.
	second := newTask(t, cfg, who, "Rebook the Zurich trip", "normal")
	secondRecord := servedPage(t, cfg, who, acmeHost, "/app/task/tasks/"+second)
	if code, _, fault := askForm(t, cfg, who, acmeHost, drawnAddress(t, secondRecord, "/saved/save"), saveForm(t, secondRecord, second)); code != http.StatusSeeOther {
		t.Fatalf("keep a copy of the second row: %d (%s)", code, fault)
	}
	keptList := drawnAddress(t, record, "/saved?module=")
	unfiltered, _, _ := strings.Cut(keptList, "?")
	target, err := url.Parse("http://" + cfg.Server.Addr)
	if err != nil {
		t.Fatal(err)
	}
	cookieOrigin := *target
	cookieOrigin.Host = acmeHost
	proxy := httputil.NewSingleHostReverseProxy(target)
	direct := proxy.Director
	proxy.Director = func(r *http.Request) {
		direct(r)
		r.Host = acmeHost
		for _, cookie := range who.Jar.Cookies(&cookieOrigin) {
			r.AddCookie(cookie)
		}
	}
	preview := httptest.NewServer(proxy)
	t.Cleanup(preview.Close)
	work, err := os.MkdirTemp("/tmp", "pkit-floor-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Minute)
	defer cancel()
	paths := []string{"/app", "/app/task/tasks", "/app/task/tasks/" + id, "/app/task/tasks/new", "/app/task/tasks/" + id + "/edit",
		unfiltered, keptList}
	if gate := os.Getenv("PLATFORMKIT_SHELL_DESIGN_GATE"); gate != "" {
		for _, path := range paths {
			cmd := exec.CommandContext(ctx, "python3", gate, preview.URL+path, "--widths", "390,1440")
			cmd.Env = append(os.Environ(), "TMPDIR="+work)
			output, err := cmd.CombinedOutput()
			t.Logf("python3 %s %s%s --widths 390,1440\n%s", gate, preview.URL, path, output)
			if err != nil {
				t.Errorf("external design floor %s: %v", path, err)
			}
		}
	}
	cmd := exec.CommandContext(ctx, "node", "testdata/shell_design_floor.mjs", preview.URL, id)
	cmd.Env = append(os.Environ(), "TMPDIR="+work)
	output, err := cmd.CombinedOutput()
	t.Logf("served design floor:\n%s", output)
	if err != nil {
		t.Fatalf("rendered design floor: %v", err)
	}
	var result struct {
		Measurements int `json:"measurements"`
		Pages        []struct {
			Path     string   `json:"path"`
			Width    int      `json:"width"`
			Locale   string   `json:"locale"`
			Theme    string   `json:"theme"`
			Refusals []string `json:"refusals"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Measurements != 40 || len(result.Pages) != 40 {
		t.Fatalf("incomplete browser result: %v, measurements=%d", err, result.Measurements)
	}
	seen := map[string]bool{}
	for _, p := range result.Pages {
		if !slices.Contains(paths, p.Path) || p.Width != 390 && p.Width != 1440 || p.Locale != "en" && p.Locale != "pt-PT" || p.Theme != "light" && p.Theme != "dark" {
			t.Fatalf("invalid measurement identity: %+v", p)
		}
		key := fmt.Sprintf("%s/%d/%s/%s", p.Path, p.Width, p.Locale, p.Theme)
		if seen[key] || len(p.Refusals) != 0 {
			t.Fatalf("duplicate or refused measurement %s: %v", key, p.Refusals)
		}
		seen[key] = true
	}
}
