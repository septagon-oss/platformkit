package main

import (
	"net/http"
	"strings"
	"testing"
)

// TestTheNoticeListShowsOneTenantsNoticesToNobodyAtAnother: the in-app list is a
// person's own rows inside their own tenant. A notice raised for Acme's
// administrator is on Acme's list, and Globex's administrator — signed in at
// Globex's host, at the same address — is shown a list without it.
func TestTheNoticeListShowsOneTenantsNoticesToNobodyAtAnother(t *testing.T) {
	// The reset notice is raised by the worker that mails the link, so this
	// installation is one that named a mail host.
	cfg, acme, _ := startedMailing(t)
	secondTenant(t, cfg, "globex", "root@globex.localhost")
	globex := signIn(t, cfg, globexHost, "root@globex.localhost", adminPass)

	if code, body := do(t, cfg, &http.Client{}, http.MethodPost, acmeHost, "/api/v1/public/auth/password/forgot",
		`{"email":"`+adminEmail+`"}`); code != http.StatusOK {
		t.Fatalf("asking for a reset link at acme = %d %s", code, body)
	}
	const notice = "Reset your password"
	const inbox = "/app/notification/notifications"
	var mine string
	eventually(t, "acme's administrator to be told", func() bool {
		_, _, mine = askNavigate(t, cfg, acme, acmeHost, inbox)
		return strings.Contains(mine, notice)
	})

	status, _, theirs := askNavigate(t, cfg, globex, globexHost, inbox)
	if status != http.StatusOK || !strings.Contains(theirs, `data-controller="live"`) {
		t.Fatalf("GET %s at globex = %d, want globex's own list: %s", inbox, status, firstLineOf(theirs))
	}
	if strings.Contains(theirs, notice) {
		t.Errorf("globex's administrator is shown a notice addressed to acme's")
	}
}
