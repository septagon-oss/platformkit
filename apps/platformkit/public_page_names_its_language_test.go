package main

// The public half of decision 0069 on the reference app's content pages: a tenant
// served in English and Portuguese publishes a page with a Portuguese translation,
// and a visitor who asks for Portuguese is given the Portuguese text inside a
// document that says it is Portuguese, with the alternates a search engine needs —
// one per complete locale and an x-default.
//
// Reaching the page is checked through what any answer prints (a 200 that carries
// the page's title in one of its two languages), so the assertions below only read
// what the translated page itself would print.

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func TestAPublicPageNamesItsLanguageAndItsAlternates(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport, options.Log = memory.New(), quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	declareLocale(t, cfg, who, installationTenantID(t, cfg, who, "acme"), `{"default":"en","supported":["pt-PT"]}`)

	input, _ := json.Marshal(map[string]any{"slug": "about-translated", "title": "About us", "kind": "page", "body": "Our story."})
	code, body := do(t, cfg, who, http.MethodPost, acmeHost, contentPath, string(input))
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")
	translation, _ := json.Marshal(map[string]any{"lang": "pt-PT", "values": map[string]string{"title": "Sobre nós", "body": "A nossa história."}})
	code, body = do(t, cfg, who, http.MethodPost, acmeHost, contentPath+"/"+id+"/translate", string(translation))
	if code != http.StatusOK {
		t.Fatalf("translate = %d %s", code, body)
	}
	publisher := signIn(t, cfg, acmeHost, publisherIn(t, cfg, who, "publisher@acme.localhost"), publisherPass)
	code, body = do(t, cfg, publisher, http.MethodPost, acmeHost, contentPath+"/"+id+"/publish", "")
	if code != http.StatusOK {
		t.Fatalf("publish = %d %s", code, body)
	}

	status, html := getLanguage(t, cfg, acmeHost, "/about-translated?lang=pt-PT", "pt-PT,pt;q=0.9")
	if status != http.StatusOK || !(strings.Contains(html, "About us") || strings.Contains(html, "Sobre nós")) {
		t.Fatalf("GET /about-translated?lang=pt-PT = %d, want the published page: %s", status, firstLineOf(html))
	}
	if !strings.Contains(html, `<html lang="pt-PT"`) {
		t.Errorf("the Portuguese page does not declare lang=\"pt-PT\" on its document: %s", firstLineOf(html))
	}
	if !strings.Contains(html, "Sobre nós") {
		t.Errorf("the Portuguese page does not carry the reviewed Portuguese title")
	}
	for _, alternate := range []string{`hreflang="en"`, `hreflang="pt-PT"`, `hreflang="x-default"`} {
		if !strings.Contains(html, alternate) {
			t.Errorf("the public page has no %s alternate", alternate)
		}
	}
}
