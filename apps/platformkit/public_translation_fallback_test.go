package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

func publishedTranslationPage(t *testing.T, values map[string]string) (config.Config, *http.Client, string) {
	t.Helper()
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport, options.Log = memory.New(), quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	declareLocale(t, cfg, who, installationTenantID(t, cfg, who, "acme"), `{"default":"en","supported":["pt-PT"]}`)
	code, body := do(t, cfg, who, http.MethodPost, acmeHost, contentPath,
		`{"slug":"translated-story","title":"Our story","kind":"page","body":"The original English paragraph."}`)
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")
	if values != nil {
		input, err := json.Marshal(map[string]any{"lang": "pt-PT", "values": values})
		if err != nil {
			t.Fatal(err)
		}
		code, body = do(t, cfg, who, http.MethodPost, acmeHost, contentPath+"/"+id+"/translate", string(input))
		if code != http.StatusOK {
			t.Fatalf("translate = %d %s", code, body)
		}
	}
	publisher := signIn(t, cfg, acmeHost, publisherIn(t, cfg, who, "publisher@acme.localhost"), publisherPass)
	code, body = do(t, cfg, publisher, http.MethodPost, acmeHost, contentPath+"/"+id+"/publish", "")
	if code != http.StatusOK {
		t.Fatalf("publish = %d %s", code, body)
	}
	return cfg, who, id
}

func TestPublicTranslationFallsBackOnlyForTheMissingField(t *testing.T) {
	cfg, _, _ := publishedTranslationPage(t, map[string]string{"title": "A nossa história"})
	status, body := getLanguage(t, cfg, acmeHost, "/translated-story?lang=pt-PT", "pt-PT")
	if status != http.StatusOK {
		t.Fatalf("public read = %d", status)
	}
	if !strings.Contains(body, "A nossa história") {
		t.Error("reviewed Portuguese title is discarded when only the body translation is missing")
	}
	if !strings.Contains(body, "The original English paragraph.") {
		t.Error("missing body translation must fall back to the English source")
	}
	if !strings.Contains(body, `<html lang="pt-PT"`) {
		t.Error("the requested Portuguese document must mark its English fallback fragment separately")
	}
	document, err := html.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var checkFallback func(*html.Node, string)
	checkFallback = func(node *html.Node, language string) {
		for _, attr := range node.Attr {
			if attr.Key == "lang" {
				language = attr.Val
			}
		}
		if node.Type == html.TextNode && strings.Contains(node.Data, "The original English paragraph.") && language != "en" {
			t.Errorf("English fallback inherits language %q, want en", language)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			checkFallback(child, language)
		}
	}
	checkFallback(document, "")
}
