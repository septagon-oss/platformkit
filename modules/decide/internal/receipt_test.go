package internal_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/decide/contracts"
	"github.com/septagon-oss/platformkit/modules/decide/contracts/decidetest"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestAQuestionReceiptRechecksCurrentAuthority(t *testing.T) {
	for _, column := range []string{"read", "manage", "active", "credential", "unavailable"} {
		t.Run(column, func(t *testing.T) {
			w := newWorld(t)
			w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
			_, handler := mountedDecide(t, w)
			path := "/api/v1/decide/questions/" + decidetest.Q.String() + "/close"
			first := request(t, handler, http.MethodPost, path, `{"ifRevision":1}`, "close.intent")
			if first.Code != http.StatusOK {
				t.Fatalf("close: %d %s", first.Code, first.Body.String())
			}
			retry := request(t, handler, http.MethodPost, path, `{"ifRevision":1}`, "close.intent")
			if retry.Code != http.StatusOK || retry.Body.String() != first.Body.String() {
				t.Fatalf("authorized recovery: %d %s", retry.Code, retry.Body.String())
			}
			want := http.StatusForbidden
			switch column {
			case "credential":
				w.ctx = tenancy.WithPrincipal(w.ctx, tenancy.Principal{UserID: decidetest.Alice, Permissions: []string{contracts.PermissionManage}})
			case "unavailable":
				want = http.StatusServiceUnavailable
				if _, err := w.admin.ExecContext(t.Context(), "DROP TABLE decide_test_people"); err != nil {
					t.Fatal(err)
				}
			default:
				if _, err := w.admin.ExecContext(t.Context(), `UPDATE decide_test_people SET "`+column+`"=false WHERE id=$1`, decidetest.Alice); err != nil {
					t.Fatal(err)
				}
			}
			denied := request(t, handler, http.MethodPost, path, `{"ifRevision":1}`, "close.intent")
			if denied.Code != want || strings.Contains(denied.Body.String(), `"revision"`) {
				t.Fatalf("revoked %s recovered a row: %d %s", column, denied.Code, denied.Body.String())
			}
			var revisions, closed int
			if err := w.admin.QueryRowContext(t.Context(), `SELECT (SELECT revision FROM decide_questions WHERE id=$1), (SELECT count(*) FROM platformkit_outbox WHERE name='decide.closed')`, decidetest.Q).Scan(&revisions, &closed); err != nil {
				t.Fatal(err)
			}
			if revisions != 2 || closed != 1 {
				t.Fatalf("recovery changed state: revision=%d closed=%d", revisions, closed)
			}
		})
	}
}

func TestInviteAndUndoCredentialsAreNotRetainedForHTTPReplay(t *testing.T) {
	for _, action := range []string{"issue-invite", "yes"} {
		t.Run(action, func(t *testing.T) {
			w := newWorld(t)
			w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				snap := decidetest.Seed()
				snap.Question.AllowLinkJoin = true
				return put(ctx, tx, snap)
			})
			_, handler := mountedDecide(t, w)
			path := "/api/v1/decide/questions/" + decidetest.Q.String() + "/issue-invite"
			body := `{"ifRevision":1,"expiresAt":"` + decidetest.At.Add(time.Hour).Format(time.RFC3339) + `"}`
			field := "token"
			if action == "yes" {
				path = "/api/v1/decide/cards/" + decidetest.B.String() + "/yes"
				body = `{"questionId":"` + decidetest.Q.String() + `","ifRevision":0}`
				field = "undoToken"
			}
			first := request(t, handler, http.MethodPost, path, body, "credential.intent")
			if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `"`+field+`":`) {
				t.Fatalf("credential issuance: %d %s", first.Code, first.Body.String())
			}
			retry := request(t, handler, http.MethodPost, path, body, "credential.intent")
			if retry.Code != http.StatusConflict || strings.Contains(retry.Body.String(), `"`+field+`":`) {
				t.Fatalf("credential replay: %d %s", retry.Code, retry.Body.String())
			}
			var retained int
			if err := w.admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_idempotency WHERE recorded OR octet_length(body)>0`).Scan(&retained); err != nil {
				t.Fatal(err)
			}
			if retained != 0 {
				t.Fatalf("retained credential answers: %d", retained)
			}
		})
	}
}

func TestAGeneratedVoteFormDoesNotRetainItsUndoCredential(t *testing.T) {
	w := newWorld(t)
	w.run(t, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return put(ctx, tx, decidetest.Seed()) })
	api, handler := mountedDecide(t, w)
	shell := page.Shell{Tag: "decide", Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}
	for _, resource := range api.Resources() {
		screens.Mount(api.Surfaces("decide").App, shell, screens.Options{Workspace: "/app"}, resource)
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	path := api.Resources()[1].Screen + "/" + decidetest.B.String() + "/yes"
	body := url.Values{"questionId": {decidetest.Q.String()}, "ifRevision": {"0"}}.Encode()
	first := request(t, handler, http.MethodPost, path, body, "form.intent")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), `name="token"`) ||
		!strings.Contains(first.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("form outcome: %d %s", first.Code, first.Body.String())
	}
	retry := request(t, handler, http.MethodPost, path, body, "form.intent")
	if retry.Code != http.StatusConflict || strings.Contains(retry.Body.String(), `name="token"`) {
		t.Fatalf("form replay: %d %s", retry.Code, retry.Body.String())
	}
}
