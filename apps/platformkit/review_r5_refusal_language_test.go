package main

// A reviewer's case, T-0111 review round 5 (2026-09-29).
//
// The branch ships thirteen refusal sentences in pt-PT (`ui/page/messages/pt-PT.json`,
// every key `fault.`), and round 3's and round 4's work on them is titled
// "an outage refusal is answered in the language the tenant is served in". Every case
// that proves it composes its own `page.Shell` — with `Messages` — and calls
// `page.FaultHandler` or `page.Serve` directly. Nothing asks the running application.
//
// The running application answers a person who navigated in English, in a deployment
// whose tenant serves Portuguese. Two separate lines are why, and the two sub-cases
// below name them by the path a refusal actually takes:
//
//   - `apps/platformkit/faultPage()` hands `page.FaultHandler` a `page.Shell` with
//     `Chrome`, `Frame`, `Back` and `BackLabel`, and no `Messages`. Every refusal a
//     guard makes ahead of routing — the 405 of the wrong verb, the 403 of a cross-site
//     write, the 503 a guard answers when its own decision could not be made, which is
//     the case 4c8b3cf exists for — is rendered by that shell, and `refusalLocale` with
//     no catalog returns nil, which `fault.go` documents as "shown the kernel's English
//     sentence". Measured at the delivered head:
//
//       POST /app  Accept: text/html  Accept-Language: pt-PT
//       HTTP/1.1 405 Method Not Allowed          (no Content-Language)
//       html lang="en"  "this address does not accept POST requests"
//
//     and with `Messages: catalogues()` added to that one literal, the same request:
//
//       HTTP/1.1 405 Method Not Allowed
//       Content-Language: pt-PT
//       Vary: Accept-Language
//       html lang="pt-PT"  "Este endereço não aceita este pedido."
//
//   - `page.Serve` turns a handler's own 4xx into `Fault(status, detail, s.Back,
//     s.BackLabel)` — the exported entry point, which takes no locale at all, unlike
//     the `fault(status, detail, loc, …)` that `FaultHandler` uses. So a module's
//     refusal is English even in the shell that *does* ship a catalog (the admin shell
//     gets `Messages: installed` at modules.go:219). Measured at the delivered head, a
//     GET of an address whose handler answers `problem.NotFound`:
//
//       HTTP/1.1 404 Not Found                   (no Content-Language)
//       html lang="en"  "there is no page at /_no_such_page_"
//
//     and the mutant's answer is unchanged, which is what keeps this case separate
//     from the one above: wiring the catalog into the failure page does not reach it.
//
// What is *not* claimed here, because the page does not do it: no refusal declares
// Portuguese over an English line. `ui/document.Fault` sets `Language: "en"`, so the
// delivered page is honest — it is honest in the wrong language, and the copy the
// composition paid for is never read.
//
// Both assertions are about the fixed behaviour, and both are reached through what the
// refusal says about itself whatever its language — its status and its content type —
// never through the English sentence the defect prints. The first block below is the
// reachability probe: it asks for a page the branch already translates and insists on
// Portuguese, so a tenant whose languages were not live at all could not pass it.

import (
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// navigated reads one address the way a browser does — an explicit `text/html`, which
// is the only Accept that makes the kernel answer a refusal as a page (kit/httpx/fault.go
// refuses to guess for `*/*`) — in the language of a person who writes in Portuguese.
// It answers the status, the content type, `Content-Language` and the body.
func navigatedTo(t *testing.T, cfg config.Config, method, host, path string) (int, string, string, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, "http://"+cfg.Server.Addr+path, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Host = host
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "pt-PT")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s at %s: %v", method, path, host, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read %s %s: %v", method, path, err)
	}
	return res.StatusCode, res.Header.Get("Content-Type"), res.Header.Get("Content-Language"), string(body)
}

// declaredLanguage is the language the document says its own words are in.
func refusedLanguage(t *testing.T, body string) string {
	t.Helper()
	m := regexp.MustCompile(`(?s)<html[^>]*\blang="([^"]*)"`).FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page declared no language at all: %s", firstLine(body))
	}
	return m[1]
}

// TestAPersonWhoNavigatedAndWasRefusedIsAnsweredInTheTenantsLanguage asks the reference
// application, with the failure page the reference application actually registers,
// whether a refusal of a Portuguese person is in Portuguese.
func TestAPersonWhoNavigatedAndWasRefusedIsAnsweredInTheTenantsLanguage(t *testing.T) {
	path, cfg := configure(t)
	install(t, path) // acme, bootstrapped --language pt-PT: the tenant serves en and pt-PT
	c := compose(cfg)
	// appOptions, not an Options literal of this file's own: it is the composition every
	// entry point of the binary shares, and the one line that puts the product's failure
	// page — faultPage() — in front of every guard. A case that hand-wired its own shell
	// would be testing this file, which is the mistake the cases above it made.
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	// Reachability, and it is a control rather than a hope: the same tenant, the same
	// request headers, one address a page owns. If this is not Portuguese, the tenant's
	// languages are not live in this process and the two cases below mean nothing.
	status, contentType, header, body := navigatedTo(t, cfg, http.MethodGet, acmeHost, "/app/admin/login")
	if status != http.StatusOK {
		t.Fatalf("the sign-in page = %d: %s", status, short(body))
	}
	if !strings.HasPrefix(contentType, "text/html") {
		t.Fatalf("the sign-in page is not a page: %s", contentType)
	}
	if got := refusedLanguage(t, body); got != "pt-PT" {
		t.Fatalf("this tenant declared pt-PT and its sign-in page declares %q (%s): the fixture is not the case",
			got, header)
	}

	// A guard refused a request that never reached a handler. This is the refusal
	// 4c8b3cf's subject line is about.
	t.Run("a guard refuses ahead of routing", func(t *testing.T) {
		status, contentType, header, body := navigatedTo(t, cfg, http.MethodPost, acmeHost, "/app")
		if status != http.StatusMethodNotAllowed {
			t.Fatalf("POST /app = %d: %s", status, short(body))
		}
		if !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("a navigating caller was answered %s, so no page was rendered to refuse them", contentType)
		}
		if header != "pt-PT" {
			t.Errorf("the refusal of a person the tenant serves in Portuguese carries Content-Language %q: "+
				"apps/platformkit/faultPage() gives page.FaultHandler a Shell with no Messages, so "+
				"refusalLocale has no catalog to negotiate and the ui/page refusal catalogue is never read",
				header)
		}
		if got := refusedLanguage(t, body); got != "pt-PT" {
			t.Errorf("the refusal page declares lang=%q to a person the tenant serves in pt-PT: the "+
				"fault.* sentences this branch shipped are copy nothing reads at the application "+
				"that shipped them: %s", got, short(body))
		}
	})

	// A module's own handler refused. Different line, different cure: Serve builds this
	// one with page.Fault, which takes no locale, whatever the shell was composed with.
	t.Run("a handler refuses its own page", func(t *testing.T) {
		status, contentType, header, body := navigatedTo(t, cfg, http.MethodGet, acmeHost, "/_no_reviewer_here_")
		if status != http.StatusNotFound {
			t.Fatalf("GET /_no_reviewer_here_ = %d: %s", status, short(body))
		}
		if !strings.HasPrefix(contentType, "text/html") {
			t.Fatalf("a navigating caller was answered %s, so no page was rendered to refuse them", contentType)
		}
		if header != "pt-PT" {
			t.Errorf("a 404 rendered for a tenant served in Portuguese carries Content-Language %q: page.Serve "+
				"turns a handler's 4xx into page.Fault(status, detail, back, backLabel), which is given no "+
				"locale, while page.FaultHandler uses fault(…, loc, …) which is", header)
		}
		if got := refusedLanguage(t, body); got != "pt-PT" {
			t.Errorf("a person the tenant serves in Portuguese was refused a page in lang=%q: %s", got, short(body))
		}
	})
}
