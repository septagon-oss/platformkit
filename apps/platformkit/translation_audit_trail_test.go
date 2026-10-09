package main

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
)

// A translation written through a record's own door reaches the tenant's trail as
// translation.updated, with the person who wrote it and the request that caused it,
// in the composed reference application rather than in the module's own tables.
func TestATranslationWriteIsInTheTrailWithItsActorAndRequest(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	options := appOptions(cfg, c, app.All)
	options.Transport, options.Log = memory.New(), quiet()
	start(t, cfg, c.modules, options)
	who := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	trailIncluded(t, cfg, who)
	declareLocale(t, cfg, who, installationTenantID(t, cfg, who, "acme"), `{"default":"en","supported":["pt-PT"]}`)

	input, _ := json.Marshal(map[string]any{"slug": "trail-translated", "title": "About us", "kind": "page", "body": "Our story."})
	code, body := do(t, cfg, who, http.MethodPost, acmeHost, contentPath, string(input))
	if code != http.StatusCreated {
		t.Fatalf("create = %d %s", code, body)
	}
	translation, _ := json.Marshal(map[string]any{"lang": "pt-PT", "values": map[string]string{"title": "Sobre nós"}})
	code, body = do(t, cfg, who, http.MethodPost, acmeHost, contentPath+"/"+field(t, body, "id")+"/translate", string(translation))
	if code != http.StatusOK {
		t.Fatalf("translate = %d %s", code, body)
	}
	row := waitForAudit(t, cfg, who, "translation.updated")
	if row["actor"] == nil || row["actor"] == "" {
		t.Errorf("the trail row for the translation names no actor: %v", row)
	}
	if row["requestId"] == nil || row["requestId"] == "" {
		t.Errorf("the trail row for the translation carries no request id: %v", row)
	}
}
