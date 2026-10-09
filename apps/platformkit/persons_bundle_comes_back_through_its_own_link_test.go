package main

// A person's export, asked over the wire and opened over the wire.
//
// modules/subject's own cases prove the module: the request row and its event, the walk's
// order, the token's hash, the one answer for used, forged and expired. What they cannot
// prove is the thing only the composition has — that the zip the walk stored through
// `file.Service` comes back out of the file module through the subject module's public
// door, at the tenant's own host, to a caller with no session and nothing but a token.
// House rule 3 is the reason: no module may read across the boundary this test crosses.
//
// The worker is driven rather than waited for: `composition` hands over the manifests, the
// named job is found and run once, which is what the scheduler would do a minute later.

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestAPersonsBundleComesBackThroughTheLinkTheCompositionMints(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	// Somebody with a session and no grant at all: the export of oneself asks no
	// permission, and this is the case that says so at the door rather than in a comment.
	acme := acmeTenant(t, cfg)
	email := "bundle.persona@acme.localhost"
	provisionAs(t, cfg, c, acme, email, "member")
	person := signIn(t, cfg, acmeHost, email, adminPass)

	code, body := do(t, cfg, person, http.MethodPost, acmeHost, exportsPath, `{"reason":"my copy"}`)
	if code < 200 || code > 299 {
		t.Fatalf("asking for a copy of one's own data = %d %s", code, body)
	}
	request := field(t, body, "id")

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	// The walk, run once as the worker runs it. A tick of the scheduler may have taken the
	// row first — the composition runs the job — so the case asks the question it actually
	// cares about (is there a bundle?) rather than who got there first.
	runExportWalk(t, c, conn)
	if code, body := do(t, cfg, person, http.MethodGet, acmeHost, exportsPath+"/"+request, ""); code != http.StatusOK {
		t.Fatalf("reading the request back = %d %s", code, body)
	} else if field(t, body, "state") != "ready" {
		// A scheduler tick may have taken the row first, or not yet reached it; either way
		// the question is whether there is a bundle now, and running the walk again is the
		// honest way to ask.
		runExportWalk(t, c, conn)
		if code, body := do(t, cfg, person, http.MethodGet, acmeHost, exportsPath+"/"+request, ""); code != http.StatusOK || field(t, body, "state") != "ready" {
			t.Fatalf("the walk left the request at %d %s, want it ready", code, body)
		}
	}

	code, body = do(t, cfg, person, http.MethodPost, acmeHost, exportsPath+"/"+request+"/link", "")
	if code != http.StatusOK {
		t.Fatalf("minting the person's own link = %d %s; the walk files the bundle and the notice, and this is where the door opens", code, body)
	}
	token := field(t, body, "token")
	if len(token) != 64 {
		t.Fatalf("the minted token is %q; it is 32 random bytes in hex and nothing a reader of the table could guess", token)
	}

	// The public door, anonymously, at the tenant's own host: the zip's own bytes, which is
	// the file module's answer reaching a person through the subject module's route.
	address := "/api/v1/public/subject/bundles/" + token
	code, zip := do(t, cfg, nil, http.MethodGet, acmeHost, address, "")
	if code != http.StatusOK {
		t.Fatalf("the seven-day link answered %d at %s", code, address)
	}
	if !strings.HasPrefix(zip, "PK") {
		t.Errorf("the bundle is %d bytes and does not start with a zip's magic: %q", len(zip), zip[:min(24, len(zip))])
	}
	if !strings.Contains(zip, "manifest.json") {
		t.Errorf("the bundle carries no manifest entry: %d bytes returned", len(zip))
	}

	// Single use, at the door and not only in the command: the same address, asked again,
	// answers the refusal a forged token gets, and says nothing about which it is.
	if code, again := do(t, cfg, nil, http.MethodGet, acmeHost, address, ""); code != http.StatusNotFound {
		t.Errorf("a link opened twice = %d %s, want 404", code, brief(again))
	}
	if code, forged := do(t, cfg, nil, http.MethodGet, acmeHost,
		"/api/v1/public/subject/bundles/"+strings.Repeat("0", 64), ""); code != http.StatusNotFound {
		t.Errorf("a forged token = %d %s, want the same 404 the used one got", code, brief(forged))
	}
}

// runExportWalk runs the composed `subject-export` job once, as the worker would.
func runExportWalk(t *testing.T, c composition, conn *db.Conn) {
	t.Helper()
	for _, m := range c.modules {
		for _, job := range m.Jobs {
			if job.Name != "subject-export" {
				continue
			}
			if err := job.Run(context.Background(), conn); err != nil {
				t.Fatalf("the export walk: %v", err)
			}
			return
		}
	}
	t.Fatal(`no composed job named "subject-export": the module declares none, so nothing produces a bundle`)
}
