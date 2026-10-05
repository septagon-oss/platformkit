package main

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/locale/providers/pseudo"
)

// The public site's refusal is a fault page the reference application serves, and
// its way out is a word a visitor reads. Either that word reaches a catalogue, or the
// pseudo-locale gate — which renders "the fault pages" — measures the page and names
// it in its report. A way out that is neither is copy nobody can see is untranslated.
//
// Only the refusal's own way back is read: the link the shell gives `Back: "/"`,
// which is the anchor after the refusal's block. The frame's brand link, if the
// public shell ever grows one, is the tenant's name and not this case's business.
func TestThePublicRefusalsWayBackIsTranslatedOrMeasured(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	installed := catalogues()
	c := composeCopy(cfg, installed, pseudo.Wrap(installed, nil))
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)

	code, _, body := get(t, cfg, nil, "/no-such-page-at-all")
	if code != http.StatusNotFound {
		t.Fatalf("an unknown public address answered %d", code)
	}
	document, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var way []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key == "href" && a.Val == "/" {
					var text strings.Builder
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.TextNode {
							text.WriteString(c.Data)
						}
					}
					way = append(way, strings.TrimSpace(text.String()))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(document)
	if len(way) == 0 {
		t.Fatalf("the public refusal offers no way back to the site: %s", body)
	}
	label := way[len(way)-1]
	if pseudo.Wrapped(label) {
		return
	}
	if report := runGateForTypedData(t); !strings.Contains(report, `"`+label+`"`) {
		t.Errorf("the public refusal's way back reads %q, which no catalogue answered, and the "+
			"pseudo-locale gate's report never names it: the public refusal is not in the measured set", label)
	}
}
