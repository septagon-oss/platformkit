package main

// The AsyncAPI document describes the payload the broker actually carries.
//
// AsyncAPI 3.0.0 says a message's `payload` member IS a Schema Object — the
// message body's schema, read where it is. kit/app/asyncapi.go writes
// `payload: {"schema": {…}}`, and the golden file carries that wrapper for all
// fifty channels. The document still validates against the published AsyncAPI
// 3.0.0 metaschema (its schema definition accepts any keyword), which is exactly
// why nothing here caught it: a validator or a code generator pointed at
// `channels[user.invited].messages[user.invited].payload` reads a schema whose
// only member is the unknown keyword `schema`, and so constrains nothing.
//
// Measured against the checked-in document with a real validator:
//
// payload member  vs {"anything":123}  -> 0 errors   (the contract is inert)
// payload.schema  vs {"anything":123}  -> 4 errors   (userId, email, status, at)
//
// The same wrapping is why the golden test could assert
// `payload.schema.type == "object"` and call the catalogue covered: it reads the
// wrapper, not what an integrator would read. The nested `$schema`
// (2020-12) inside a document whose schema dialect is draft-07 is the second
// half of the same carelessness.
//
// Emit the projection as `payload` itself — and drop the `$schema` member, whose
// dialect is not the enclosing document's — and every assertion below passes.

import (
	"encoding/json"
	"os"
	"sort"
	"testing"
)

func TestTheMessagePayloadIsTheSchemaAnIntegratorReads(t *testing.T) {
	body, err := os.ReadFile(asyncapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v", asyncapiGolden, err)
	}
	var doc struct {
		Channels map[string]struct {
			Messages map[string]struct {
				Payload map[string]any `json:"payload"`
			} `json:"messages"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("%s is not JSON: %v", asyncapiGolden, err)
	}
	if len(doc.Channels) == 0 {
		t.Fatal("the document has no channels")
	}
	var wrapped []string
	for name, channel := range doc.Channels {
		for messageName, message := range channel.Messages {
			payload := message.Payload
			if payload == nil {
				t.Errorf("%s/%s has no payload member", name, messageName)
				continue
			}
			if _, doubled := payload["schema"]; doubled {
				wrapped = append(wrapped, name)
				continue
			}
			// The declaration the module made has to be readable where the
			// standard says it is: an object with the payload's members.
			if payload["type"] != "object" {
				t.Errorf("%s/%s payload is %v, want the payload's own object schema", name, messageName, payload["type"])
			}
			if _, ok := payload["properties"]; !ok {
				t.Errorf("%s/%s payload carries no properties: %v", name, messageName, payload)
			}
			if _, ok := payload["$schema"]; ok {
				t.Errorf("%s/%s payload names a $schema dialect the enclosing document does not use", name, messageName)
			}
		}
	}
	if len(wrapped) > 0 {
		sort.Strings(wrapped)
		t.Errorf("%d message(s) wrap the payload schema in a `schema` member no AsyncAPI or JSON Schema reader descends into; %v…",
			len(wrapped), wrapped[:min(5, len(wrapped))])
	}
}
