package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

func TestContentBodyIsRichTextInTheServedCatalog(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	for _, at := range []string{"/api/v1/app/resources", "/api/v1/admin/resources"} {
		code, body := do(t, cfg, who, http.MethodGet, acmeHost, at, "")
		if code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", at, code, body)
		}
		var catalog struct {
			Version   int `json:"catalogVersion"`
			Resources []struct {
				Module, Entity string
				Fields         []struct {
					Name, Widget string
					MaxLength    int
				}
			} `json:"resources"`
		}
		if err := json.Unmarshal([]byte(body), &catalog); err != nil {
			t.Fatal(err)
		}
		if catalog.Version != 2 {
			t.Errorf("catalog version = %d", catalog.Version)
		}
		found := false
		for _, resource := range catalog.Resources {
			if resource.Module != "content" || resource.Entity != "content" {
				continue
			}
			for _, field := range resource.Fields {
				if field.Name == "body" {
					found = true
					if field.Widget != "richtext" || field.MaxLength != 262144 {
						t.Errorf("content body = %+v", field)
					}
				}
			}
		}
		if !found {
			t.Errorf("GET %s has no content body", at)
		}
	}
}

func TestRichTextContentWriteRenderAndRefusal(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport = memory.New()
	options.Log = quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)

	source := "## Opening hours  \n\n- Monday\n- Tuesday\n\n[Website](https://example.test)\n\n```go\nfmt.Println(1)\n```"
	normal, err := richtext.Normalise(source)
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{"slug": "rich-text-journey", "title": "Rich text journey", "kind": "page", "body": source})
	code, body := do(t, cfg, who, http.MethodPost, acmeHost, contentPath, string(input))
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	id := field(t, body, "id")
	code, body = do(t, cfg, who, http.MethodGet, acmeHost, contentPath+"/"+id, "")
	if code != http.StatusOK || field(t, body, "body") != normal {
		t.Fatalf("stored body = %d %s, want %q", code, body, normal)
	}
	code, detail := do(t, cfg, who, http.MethodGet, acmeHost, "/app/content/contents/"+id, "")
	for _, want := range []string{`data-component="prose"`, `id="pk-opening-hours"`, "<ul>", `rel="noopener noreferrer nofollow ugc"`, `language-go`} {
		if code != http.StatusOK || !strings.Contains(detail, want) {
			t.Errorf("detail missing %q: %d %s", want, code, detail)
		}
	}
	bad := "<script>alert(1)</script>\n![x](https://example.test/x.png)"
	patch, _ := json.Marshal(map[string]any{"body": bad})
	code, refused := do(t, cfg, who, http.MethodPatch, acmeHost, contentPath+"/"+id, string(patch))
	if code != http.StatusUnprocessableEntity || !strings.Contains(refused, "raw HTML") || !strings.Contains(refused, "image source") {
		t.Fatalf("refusal = %d %s", code, refused)
	}
	code, body = do(t, cfg, who, http.MethodGet, acmeHost, contentPath+"/"+id, "")
	if code != http.StatusOK || field(t, body, "body") != normal {
		t.Fatalf("refusal changed body: %d %s", code, body)
	}
	// The author is not the publisher. The same person who wrote the page is
	// refused by name, and the refusal leaves the row a draft; the page goes live
	// only when a second holder of the permission asks for it.
	code, body = do(t, cfg, who, http.MethodPost, acmeHost, contentPath+"/"+id+"/publish", "")
	if code != http.StatusConflict || !strings.Contains(body, "author") {
		t.Fatalf("self-publication = %d %s, want a 409 naming authorship", code, body)
	}
	publisher := signIn(t, cfg, acmeHost, publisherIn(t, cfg, who, "publisher@acme.localhost"), publisherPass)
	code, body = do(t, cfg, publisher, http.MethodPost, acmeHost, contentPath+"/"+id+"/publish", "")
	if code != http.StatusOK {
		t.Fatalf("publish = %d %s", code, body)
	}
	code, body = do(t, cfg, nil, http.MethodGet, acmeHost, "/api/v1/public/content/contents/rich-text-journey", "")
	if code != http.StatusOK || !strings.Contains(body, `id=\"pk-opening-hours\"`) {
		t.Fatalf("public prose = %d %s", code, body)
	}
}

// publisherPass is the passphrase the invited publisher signs in with.
const publisherPass = "a passphrase for the publisher"

// publisherIn invites somebody who may publish, sets their passphrase, and
// returns the address to sign in as. The invitation carries the role in the same
// request, so nobody is an administrator-elect with no grants for a moment.
func publisherIn(t *testing.T, cfg config.Config, as *http.Client, email string) string {
	t.Helper()
	code, body := do(t, cfg, as, http.MethodPost, acmeHost, invitePath,
		`{"email":"`+email+`","displayName":"The publisher","roles":["admin"]}`)
	if code != http.StatusCreated {
		t.Fatalf("invite %s = %d %s, want 201", email, code, body)
	}
	code, body = do(t, cfg, as, http.MethodPost, acmeHost, usersPath+"/"+field(t, body, "id")+"/set-password",
		`{"password":"`+publisherPass+`"}`)
	if code != http.StatusOK {
		t.Fatalf("set-password for %s = %d %s, want 200", email, code, body)
	}
	return email
}
