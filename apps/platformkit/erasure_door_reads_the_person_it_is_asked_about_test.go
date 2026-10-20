package main

// The erasure door and the officer's export door are asked about a person named in the
// JSON body. Asked about a person who is there, with that person's address repeated, the
// erasure erases them, and the officer's export files a request about them.

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestTheErasureDoorErasesThePersonItIsAskedAbout(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	acme := acmeTenant(t, cfg)
	email := "asked.about@acme.localhost"
	provisionAs(t, cfg, c, acme, email, "member")
	admin := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	var id string
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT id::text FROM users WHERE email = ?`, email).Row().Scan(&id)
	}); err != nil {
		t.Fatalf("which person: %v", err)
	}

	code, body := do(t, cfg, admin, http.MethodPost, acmeHost, exportsPath+"/for",
		`{"subject":"`+id+`","reason":"an officer's copy"}`)
	if code < 200 || code > 299 {
		t.Errorf("the officer's export of a person who is here = %d %s, want it filed", code, body)
	} else if got := field(t, body, "subject"); got != id {
		t.Errorf("the officer's export was filed about %s, want %s", got, id)
	}

	code, body = do(t, cfg, admin, http.MethodPost, acmeHost, erasuresPath,
		`{"subject":"`+id+`","confirm":"`+email+`"}`)
	if code < 200 || code > 299 {
		t.Fatalf("erasing a person who is here, address repeated = %d %s, want it erased", code, body)
	}
	var named int
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Raw(`SELECT count(*) FROM users WHERE id::text = ? AND lower(email) = ?`, id, email).Row().Scan(&named)
	}); err != nil {
		t.Fatalf("re-read the person: %v", err)
	}
	if named != 0 {
		t.Errorf("the erasure answered %d and the person's row still carries %s", code, email)
	}
}
