package screens_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// A command the document advertises has to be executable from the document alone.
// The web shell mounts its own POST behind every command, so a browser never asks
// which verb the JSON route answers; a native shell reads this document and posts
// what it says. A command mounted with another verb and not saying so is therefore
// invisible to exactly the client the document exists for — it meets 405 at the
// address the document just promised.
func TestACatalogCommandSaysWhichVerbItsAddressAnswers(t *testing.T) {
	post := httpx.Resource{Module: "note", Entity: "note",
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/api/v1/note/notes"},
		Commands: []httpx.Command{
			{Verb: "send", Collection: true, Endpoint: "/api/v1/note/notes/send"},
			{Verb: "languages", Collection: true, Endpoint: "/api/v1/note/notes/languages", Method: "PUT"},
		}}
	entries := screens.Describe1(post, true)
	raw, err := json.Marshal(entries.Commands)
	if err != nil {
		t.Fatal(err)
	}
	var keys []map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	if _, named := keys[0]["method"]; named {
		t.Errorf("a POST command names its verb (%v); the key means \"not POST\"", keys[0])
	}
	if keys[1]["method"] != "PUT" {
		t.Errorf("a PUT command publishes method=%v; a shell that posted would meet 405", keys[1]["method"])
	}
}
