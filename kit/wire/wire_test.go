package wire_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/wire"
)

func baseline(kind string) []byte {
	if kind == "openapi" {
		return []byte(`{"openapi":"3.1.0","paths":{"/items":{
   "get":{"operationId":"list","x-platformkit-auth":{"kind":"signed_in"},"responses":{"200":{"content":{"application/json":{"schema":{"type":"object","properties":{"name":{"type":"string","enum":["a"]}}}}}}}},
   "post":{"operationId":"create","x-platformkit-auth":{"kind":"signed_in"},"requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"kind":{"type":"string","enum":["a","b"]}}}}}},"responses":{}}
  }}}`)
	}
	return []byte(`{"asyncapi":"3.0.0","defaultContentType":"application/json","channels":{
  "item":{"address":"item.changed","messages":{"item":{"name":"changed","payload":{"type":"object","properties":{"name":{"type":"string","enum":["a"]}}}}}},
  "input":{"address":"item.create","messages":{"input":{"payload":{"type":"object","properties":{"kind":{"type":"string","enum":["a","b"]}}}}}}
 },"operations":{
  "publish":{"action":"send","channel":{"$ref":"#/channels/item"},"messages":[{"$ref":"#/channels/item/messages/item"}],"x-platformkit-auth":{"kind":"signed_in"}},
  "consume":{"action":"receive","channel":{"$ref":"#/channels/input"},"messages":[{"$ref":"#/channels/input/messages/input"}],"x-platformkit-auth":{"kind":"signed_in"}}
 }}`)
}

func at(doc map[string]any, path ...string) map[string]any {
	for _, key := range path {
		doc = doc[key].(map[string]any)
	}
	return doc
}

func mutated(t *testing.T, body []byte, change func(map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	change(doc)
	result, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func operation(doc map[string]any, kind string) map[string]any {
	if kind == "openapi" {
		return at(doc, "paths", "/items", "get")
	}
	return at(doc, "operations", "publish")
}

func shape(doc map[string]any, kind, direction string) map[string]any {
	if kind == "openapi" {
		if direction == "request" {
			return at(doc, "paths", "/items", "post", "requestBody", "content", "application/json", "schema")
		}
		return at(doc, "paths", "/items", "get", "responses", "200", "content", "application/json", "schema")
	}
	if direction == "request" {
		return at(doc, "channels", "input", "messages", "input", "payload")
	}
	return at(doc, "channels", "item", "messages", "item", "payload")
}

type ruleCase struct {
	name, rule, path, member, message string
	change                            func(map[string]any)
}

// These same mutations exercise Compare and the real Golden subprocess; their
// expected rules/diagnostics are literals, not computed by another comparator.
func ruleCases(kind string) []ruleCase {
	path, id, responseKey := "GET /items", "list", "list response 200 application/json .name"
	requestKey := "create request body application/json"
	if kind == "asyncapi" {
		path, id, responseKey = "SEND item.changed", "publish", "publish response message/item/item payload .name"
		requestKey = "consume request message/input/input payload"
	}
	return []ruleCase{
		{"removed operation", "B1", path, "operationId", fmt.Sprintf("B1 (breaking): operationId %q is no longer in the document", id), func(doc map[string]any) {
			if kind == "openapi" {
				delete(at(doc, "paths", "/items"), "get")
			} else {
				delete(at(doc, "operations"), "publish")
			}
		}},
		{"moved address", "B2", path, "route", "B2 (breaking): " + path + " is no longer in the document", func(doc map[string]any) {
			if kind == "openapi" {
				paths := at(doc, "paths")
				paths["/other"] = paths["/items"]
				delete(paths, "/items")
			} else {
				at(doc, "channels", "item")["address"] = "item.other"
			}
		}},
		{"retyped member", "B3", path, responseKey, "B3 (breaking): " + responseKey + ` changed from "type=string enum=a" to "type=integer enum=a"`, func(doc map[string]any) { at(shape(doc, kind, "response"), "properties", "name")["type"] = "integer" }},
		{"required request", "B4", map[string]string{"openapi": "POST /items", "asyncapi": "RECEIVE item.create"}[kind], requestKey + " requires kind", "B4 (breaking): " + requestKey + " requires kind is asked for and an older client never sends it", func(doc map[string]any) { shape(doc, kind, "request")["required"] = []any{"kind"} }},
		{"response enum grows", "B5", path, responseKey, "B5 (breaking): " + responseKey + ` may now answer "b", which an older reader has no branch for`, func(doc map[string]any) {
			at(shape(doc, kind, "response"), "properties", "name")["enum"] = []any{"a", "b"}
		}},
		{"request enum shrinks", "B5", map[string]string{"openapi": "POST /items", "asyncapi": "RECEIVE item.create"}[kind], requestKey + " .kind", "B5 (breaking): " + requestKey + ` .kind no longer accepts "b", which an older client sends`, func(doc map[string]any) { at(shape(doc, kind, "request"), "properties", "kind")["enum"] = []any{"a"} }},
		{"authorization", "B6", path, "x-platformkit-auth", fmt.Sprintf("B6 (breaking): %s (%s) is authorized kind=any_credential where it was kind=signed_in", path, id), func(doc map[string]any) {
			operation(doc, kind)["x-platformkit-auth"] = map[string]any{"kind": "any_credential"}
		}},
	}
}

func TestEachRule(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range ruleCases(kind) {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				old := baseline(kind)
				fresh := mutated(t, old, tc.change)
				breaks := wire.Compare(old, fresh)
				want := wire.Break{Rule: tc.rule, Path: tc.path, Member: tc.member, Message: tc.message}
				if !slices.Contains(breaks, want) {
					t.Fatalf("got %#v; want %#v", breaks, want)
				}
				if want.String() != tc.message {
					t.Fatal("String changed the diagnostic")
				}
			})
		}
	}
}

func allowance() []wire.AuthorizationAllowance {
	return []wire.AuthorizationAllowance{{From: "kind=signed_in", To: "kind=any_credential", ReviewedOn: "2026-10-06", Reason: "Sessions remain admitted"}}
}

func TestExplicitAllowances(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			old := baseline(kind)
			fresh := mutated(t, old, ruleCases(kind)[6].change)
			allowed := allowance()
			if got := wire.CompareWithAllowances(old, fresh, allowed); len(got) != 0 {
				t.Fatal(got)
			}
			if got := wire.CompareWithAllowances(fresh, old, allowed); len(got) != 1 || got[0].Rule != "B6" {
				t.Fatal(got)
			}
			permission := mutated(t, fresh, func(doc map[string]any) { at(operation(doc, kind), "x-platformkit-auth")["permission"] = "item:read" })
			if got := wire.CompareWithAllowances(old, permission, allowed); len(got) != 1 || got[0].Rule != "B6" {
				t.Fatal(got)
			}
			for _, field := range []string{"date", "reason", "same", "empty", "duplicate"} {
				bad := allowance()
				switch field {
				case "date":
					bad[0].ReviewedOn = "2026-02-30"
				case "reason":
					bad[0].Reason = " \n"
				case "same":
					bad[0].To = bad[0].From
				case "empty":
					bad[0].From = ""
				case "duplicate":
					bad = append(bad, bad[0])
				}
				index := 0
				if field == "duplicate" {
					index = 1
				}
				want := []wire.Break{{Rule: "B6", Path: "$allowances", Member: fmt.Sprintf("#%d", index), Message: fmt.Sprintf("B6 (breaking): invalid authorization allowance #%d", index)}}
				if got := wire.CompareWithAllowances([]byte("invalid"), fresh, bad); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s: %#v", field, got)
				}
			}
		})
	}
}

// featureDeclaration is the value httpx.Permission("item:read").Needing("pro") marshals
// to: the permission that gates the operation, and the plan feature beside it.
func featureDeclaration() map[string]any {
	return map[string]any{"kind": "permission", "permission": "item:read", "feature": "pro"}
}

// featureBaseline is baseline(kind) with that declaration on the operation every
// authorization case changes. It is derived rather than hand-written, so every other
// expectation in this file keeps its bytes.
func featureBaseline(t *testing.T, kind string) []byte {
	t.Helper()
	return mutated(t, baseline(kind), func(doc map[string]any) {
		operation(doc, kind)["x-platformkit-auth"] = featureDeclaration()
	})
}

// spellDeclaration rewrites one declaration's bytes in another textual order.
// encoding/json sorts object keys on the way out, so writing the bytes is the only way
// to hold two documents whose authorization maps differ in order and nothing else.
func spellDeclaration(t *testing.T, body []byte, as, order string) []byte {
	t.Helper()
	if n := bytes.Count(body, []byte(as)); n != 1 {
		t.Fatalf("%s appears %d times in the document, want exactly once", as, n)
	}
	return []byte(strings.ReplaceAll(string(body), as, order))
}

// featureCase is one pair of documents, and either the break the rules must name or the
// word that they are the same door.
type featureCase struct {
	name, path, message string
	// features counts how many times the diagnostic spells `feature=`: once when one
	// side named a plan feature and the other did not, twice for a rename. A mutation
	// that left the changed member out of what a reviewer reads fails here.
	features int
	// stale says the pair is one door written two ways: the rules accept it, and the
	// golden gate refuses it as staleness rather than as a break.
	stale   bool
	golden  func(t *testing.T) []byte
	current func(t *testing.T, golden []byte) []byte
}

// featureCases is the mutation table over the plan feature, beside ruleCases rather than
// in it: TestExplicitAllowances indexes that table, so its order is a pin.
func featureCases(kind string) []featureCase {
	path, id := "GET /items", "list"
	if kind != "openapi" {
		path, id = "SEND item.changed", "publish"
	}
	featured := func(t *testing.T) []byte { return featureBaseline(t, kind) }
	declares := func(value map[string]any) func(t *testing.T, golden []byte) []byte {
		return func(t *testing.T, golden []byte) []byte {
			return mutated(t, golden, func(doc map[string]any) {
				operation(doc, kind)["x-platformkit-auth"] = value
			})
		}
	}
	drops := func(key string) func(t *testing.T, golden []byte) []byte {
		return func(t *testing.T, golden []byte) []byte {
			return mutated(t, golden, func(doc map[string]any) {
				delete(at(operation(doc, kind), "x-platformkit-auth"), key)
			})
		}
	}
	return []featureCase{
		{"feature removed", path, fmt.Sprintf("B6 (breaking): %s (%s) is authorized kind=permission permission=item:read where it was kind=permission permission=item:read feature=pro", path, id), 1, false, featured, drops("feature")},
		{"feature added", path, fmt.Sprintf("B6 (breaking): %s (%s) is authorized kind=signed_in feature=pro where it was kind=signed_in", path, id), 1, false, func(t *testing.T) []byte { return baseline(kind) },
			declares(map[string]any{"kind": "signed_in", "feature": "pro"})},
		{"feature renamed", path, fmt.Sprintf("B6 (breaking): %s (%s) is authorized kind=permission permission=item:read feature=enterprise where it was kind=permission permission=item:read feature=pro", path, id), 2, false, featured,
			declares(map[string]any{"kind": "permission", "permission": "item:read", "feature": "enterprise"})},
		{"nothing changed", path, "", 0, false, featured, func(_ *testing.T, golden []byte) []byte { return golden }},
		{"re-rendered with the trailing newline a writer adds, bytes differing and the door not", path, "", 0, true, featured,
			func(t *testing.T, golden []byte) []byte {
				return append(mutated(t, golden, func(map[string]any) {}), '\n')
			}},
		{"the same declaration with its keys written in another order", path, "", 0, true, featured,
			func(t *testing.T, golden []byte) []byte {
				return spellDeclaration(t, golden, `{"feature":"pro","kind":"permission","permission":"item:read"}`, `{"permission":"item:read","feature":"pro","kind":"permission"}`)
			}},
	}
}

// TestFeatureDeclarationIsCompared runs the feature mutations through Compare. Each names
// the operation and both spellings of what changed. The comparator refuses a document:
// whether a tenant's plan admits the feature is kit/httpx's decision at request time,
// asked and refused there, and no case here boots anything to claim it.
func TestFeatureDeclarationIsCompared(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range featureCases(kind) {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				t.Parallel()
				old := tc.golden(t)
				breaks := wire.Compare(old, tc.current(t, old))
				if tc.message == "" {
					if len(breaks) != 0 {
						t.Fatalf("one declaration written two ways broke: %#v", breaks)
					}
					return
				}
				want := wire.Break{Rule: "B6", Path: tc.path, Member: "x-platformkit-auth", Message: tc.message}
				if !slices.Contains(breaks, want) {
					t.Fatalf("got %#v; want %#v", breaks, want)
				}
				if got := strings.Count(want.Message, "feature="); got != tc.features {
					t.Errorf("the diagnostic spells feature= %d times, want %d: %s", got, tc.features, want.Message)
				}
			})
		}
	}
}

// TestGoldenRefusesFeatureMutationsBeforeWriting runs that same table through the public
// golden path. A refused mutation is handed UPDATE_GOLDEN=1 and must leave the file
// byte-for-byte the file a reviewer approved. The pairs the rules accept are the other
// half: one written twice is the same door, and the flag is for rewriting it.
func TestGoldenRefusesFeatureMutationsBeforeWriting(t *testing.T) {
	const left = "is left as it was: the rules above stand, and UPDATE_GOLDEN=1 regenerates a document that is stale, never one that is broken"
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range featureCases(kind) {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				update := "1"
				if tc.message == "" {
					update = ""
				}
				old := tc.golden(t)
				output, onDisk, err := runGolden(t, old, tc.current(t, old), update, "")
				unchanged := bytes.Equal(onDisk, old)
				switch {
				case tc.message != "":
					if err == nil || !strings.Contains(output, tc.message) || !strings.Contains(output, left) || !unchanged {
						t.Fatalf("err=%v; unchanged=%v\n%s", err, unchanged, output)
					}
				case tc.stale:
					if err == nil || !strings.Contains(output, "is stale; run with UPDATE_GOLDEN=1") || strings.Contains(output, "B6") || !unchanged {
						t.Fatalf("err=%v; unchanged=%v\n%s", err, unchanged, output)
					}
				default:
					if err != nil || !unchanged {
						t.Fatalf("err=%v; unchanged=%v\n%s", err, unchanged, output)
					}
				}
			})
		}
	}
}

// featureAllowance is what a reviewer who approved moving one operation to a higher plan
// writes down: both whole declarations, permission and feature spelled on each side.
func featureAllowance() []wire.AuthorizationAllowance {
	return []wire.AuthorizationAllowance{{
		From: "kind=permission permission=item:read feature=pro", To: "kind=permission permission=item:read feature=enterprise",
		ReviewedOn: "2026-10-09", Reason: "The same permission, one plan tier up",
	}}
}

// TestAllowancesCoverOnlyWhatTheyName is both halves of one decision. A reviewed kind
// widening names two feature-free declarations, so it covers no feature change beside it,
// whichever way the pair is read. A reviewed feature pair covers no permission change
// that came along in the same diff: an allowance is exact, so it is never half an excuse.
func TestAllowancesCoverOnlyWhatTheyName(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		t.Run(kind, func(t *testing.T) {
			signedIn := mutated(t, baseline(kind), func(doc map[string]any) {
				operation(doc, kind)["x-platformkit-auth"] = map[string]any{"kind": "signed_in", "feature": "pro"}
			})
			anyCredential := mutated(t, signedIn, func(doc map[string]any) {
				operation(doc, kind)["x-platformkit-auth"] = map[string]any{"kind": "any_credential"}
			})
			for _, pair := range [][2][]byte{{signedIn, anyCredential}, {anyCredential, signedIn}} {
				if got := wire.CompareWithAllowances(pair[0], pair[1], allowance()); len(got) != 1 || got[0].Rule != "B6" {
					t.Fatalf("the reviewed pair covered a feature it never named: %#v", got)
				}
			}
			featured := featureBaseline(t, kind)
			upgraded := mutated(t, featured, func(doc map[string]any) {
				at(operation(doc, kind), "x-platformkit-auth")["feature"] = "enterprise"
			})
			if got := wire.CompareWithAllowances(featured, upgraded, featureAllowance()); len(got) != 0 {
				t.Fatalf("the reviewed feature pair did not apply: %#v", got)
			}
			if got := wire.CompareWithAllowances(featured, upgraded, nil); len(got) != 1 || got[0].Rule != "B6" {
				t.Fatalf("a feature moved with nothing reviewed: %#v", got)
			}
			if got := wire.CompareWithAllowances(upgraded, featured, featureAllowance()); len(got) != 1 || got[0].Rule != "B6" {
				t.Fatalf("the reverse pair read as the reviewed one: %#v", got)
			}
			renamed := mutated(t, upgraded, func(doc map[string]any) {
				at(operation(doc, kind), "x-platformkit-auth")["permission"] = "item:write"
			})
			if got := wire.CompareWithAllowances(featured, renamed, slices.Concat(featureAllowance(), allowance())); len(got) != 1 || got[0].Rule != "B6" {
				t.Fatalf("an allowance for half the diff covered all of it: %#v", got)
			}
		})
	}
}

// TestAuthorizationIdentitySpellings pins the canonical spelling of a declaration as
// literal diagnostics, so the order members are named in and the way a value is quoted
// are written down rather than read back off whatever the comparator emitted. The rows
// without a feature are the regression guard: they spell exactly what they spelled before
// the plan feature joined the identity, which is why a pair anyone wrote against them,
// and the composition's own reviewed pair, keep matching.
func TestAuthorizationIdentitySpellings(t *testing.T) {
	// declaring is a one-operation document whose authorization extension holds body;
	// an empty body is an operation that declares nothing.
	declaring := func(body string) []byte {
		declared := ""
		if body != "" {
			declared = `"x-platformkit-auth":` + body + ","
		}
		return []byte(`{"openapi":"3.1.0","paths":{"/items":{"get":{"operationId":"list",` + declared + `"responses":{}}}}}`)
	}
	for _, tc := range []struct {
		name            string
		golden, current string
		message         string
	}{
		{"a feature-free pair spells what it always spelled", `{"kind":"permission","permission":"a:b"}`, `{"kind":"permission","permission":"b:c"}`, "B6 (breaking): GET /items (list) is authorized kind=permission permission=b:c where it was kind=permission permission=a:b"},
		{"a kind-only pair spells what it always spelled", `{"kind":"signed_in"}`, `{"kind":"any_credential"}`, "B6 (breaking): GET /items (list) is authorized kind=any_credential where it was kind=signed_in"},
		{"a member only a hand-written document carries follows the three the server emits", `{"kind":"permission","permission":"a:b","operator_permission":"c:d"}`, `{"kind":"permission","permission":"a:b"}`, "B6 (breaking): GET /items (list) is authorized kind=permission permission=a:b where it was kind=permission permission=a:b operator_permission=c:d"},
		{"a feature is named after the permission", `{"kind":"permission","permission":"a:b"}`, `{"feature":"pro","kind":"permission","permission":"a:b"}`, "B6 (breaking): GET /items (list) is authorized kind=permission permission=a:b feature=pro where it was kind=permission permission=a:b"},
		{"the order the document writes is not the order the identity names", `{"feature":"pro","permission":"a:b","kind":"permission"}`, `{"kind":"permission","permission":"a:b","feature":"pro"}`, ""},
		{"a null feature is the omission the server marshals", `{"feature":null,"kind":"permission","permission":"a:b"}`, `{"kind":"permission","permission":"a:b"}`, ""},
		{"an empty feature is the same omission", `{"feature":"","kind":"permission","permission":"a:b"}`, `{"kind":"permission","permission":"a:b"}`, ""},
		{"an operation that declares nothing, written two ways", ``, `null`, ""},
		{"a feature that is not a string still says something", `{"feature":true,"kind":"permission","permission":"a:b"}`, `{"kind":"permission","permission":"a:b"}`, "B6 (breaking): GET /items (list) is authorized kind=permission permission=a:b where it was kind=permission permission=a:b feature=true"},
		{"a value that holds a space is quoted", `{"feature":"pro","kind":"permission","permission":"a:b"}`, `{"feature":"pro plan","kind":"permission","permission":"a:b"}`, `B6 (breaking): GET /items (list) is authorized kind=permission permission=a:b feature="pro plan" where it was kind=permission permission=a:b feature=pro`},
		{"one member cannot carry another member's text", `{"kind":"x permission=y"}`, `{"kind":"x","permission":"y"}`, `B6 (breaking): GET /items (list) is authorized kind=x permission=y where it was kind="x permission=y"`},
		{"a declaration that is not an object keeps its value", `{"kind":"public"}`, `"public"`, `B6 (breaking): GET /items (list) is authorized value="public" where it was kind=public`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			breaks := wire.Compare(declaring(tc.golden), declaring(tc.current))
			if tc.message == "" {
				if len(breaks) != 0 {
					t.Fatalf("%s over %s broke: %#v", tc.golden, tc.current, breaks)
				}
				return
			}
			want := wire.Break{Rule: "B6", Path: "GET /items", Member: "x-platformkit-auth", Message: tc.message}
			if !slices.Contains(breaks, want) {
				t.Fatalf("got %#v; want %#v", breaks, want)
			}
		})
	}
}

func TestAdditionsAndConservativeEnums(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, addition := range []string{"optional", "description", "operation", "enum growth", "enum shrink"} {
			t.Run(kind+"/"+addition, func(t *testing.T) {
				old := baseline(kind)
				current := mutated(t, old, func(doc map[string]any) {
					switch addition {
					case "optional":
						at(shape(doc, kind, "request"), "properties")["extra"] = map[string]any{"type": "string"}
					case "description":
						operation(doc, kind)["description"] = "prose"
						operation(doc, kind)["x-other"] = map[string]any{"$ref": "not a schema"}
					case "operation":
						if kind == "openapi" {
							at(doc, "paths")["/new"] = map[string]any{"post": map[string]any{"operationId": "new", "requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": []any{"new"}}}}}}}
						} else {
							at(doc, "operations")["new"] = map[string]any{"action": "receive", "channel": map[string]any{"$ref": "#/channels/item"}, "messages": []any{map[string]any{"$ref": "#/channels/item/messages/item"}}}
							shape(doc, kind, "response")["required"] = []any{"name"}
						}
					case "enum growth":
						at(shape(doc, kind, "request"), "properties", "kind")["enum"] = []any{"a", "b", "c"}
					case "enum shrink":
						at(shape(doc, kind, "response"), "properties", "name")["enum"] = []any{}
					}
				})
				got := wire.Compare(old, current)
				if strings.HasPrefix(addition, "enum ") {
					if len(got) != 1 || got[0].Rule != "B3" {
						t.Fatal(got)
					}
				} else if len(got) != 0 {
					t.Fatal(got)
				}
			})
		}
	}
}

func TestEmptyAndInvalidDocuments(t *testing.T) {
	for _, body := range []string{`{"openapi":"3.1.0","paths":{}}`, `{"asyncapi":"3.0.0","channels":{},"operations":{}}`} {
		if got := wire.Compare([]byte(body), []byte(body)); len(got) != 0 {
			t.Fatal(got)
		}
	}
	invalid := []string{"", "null", "[]", "{", "{}", `{"openapi":"2.0","paths":{}}`, `{"openapi":"3.1.0","paths":[]}`, `{"asyncapi":"3.0.0","channels":{},"operations":null}`, `{"openapi":"3.1.0","asyncapi":"3.0.0","paths":{}}`, `{"openapi":"3.1.0","paths":{"/x":{"get":null}}}`}
	for _, kind := range []string{"openapi", "asyncapi"} {
		old := baseline(kind)
		for _, body := range invalid {
			got := wire.Compare([]byte(body), []byte(body))
			if len(got) != 2 {
				t.Fatalf("%q: %#v", body, got)
			}
			for _, b := range got {
				if b.Rule != "B3" || b.Path != "$" || !strings.Contains(b.Message, "document is invalid:") {
					t.Fatal(b)
				}
			}
		}
		for _, change := range []string{"missing id", "dangling", "external", "bad escape", "collection"} {
			current := mutated(t, old, func(doc map[string]any) {
				switch change {
				case "missing id":
					if kind == "openapi" {
						delete(operation(doc, kind), "operationId")
					} else {
						at(doc, "operations")["bad id"] = operation(doc, kind)
						delete(at(doc, "operations"), "publish")
					}
				case "dangling", "external", "bad escape":
					shape(doc, kind, "response")["$ref"] = map[string]string{"dangling": "#/missing", "external": "https://invalid.example/schema", "bad escape": "#/bad~2"}[change]
				case "collection":
					if kind == "openapi" {
						operation(doc, kind)["responses"] = []any{}
					} else {
						at(doc, "channels", "item")["messages"] = []any{}
					}
				}
			})
			got := wire.Compare(old, current)
			if len(got) != 1 || got[0].Rule != "B3" || got[0].Member != "current" {
				t.Fatalf("%s/%s: %#v", kind, change, got)
			}
		}
		changedVersion := mutated(t, old, func(doc map[string]any) { doc[kind] = "3.2.0" })
		if got := wire.Compare(old, changedVersion); len(got) != 1 || got[0].Member != "format" {
			t.Fatal(got)
		}
	}
}

func TestDeterministicReadOnlyComparison(t *testing.T) {
	old := baseline("openapi")
	fresh := mutated(t, old, func(doc map[string]any) { delete(at(doc, "paths"), "/items") })
	allowed := allowance()
	oldCopy, freshCopy, allowedCopy := bytes.Clone(old), bytes.Clone(fresh), slices.Clone(allowed)
	want := wire.CompareWithAllowances(old, fresh, allowed)
	// Encoding/json orders object keys differently from the literal; the records
	// must be identical despite the insertion order of maps and goroutine order.
	reordered := mutated(t, old, func(map[string]any) {})
	for i := range 16 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			if got := wire.CompareWithAllowances(reordered, fresh, allowed); !reflect.DeepEqual(got, want) {
				t.Fatal(got)
			}
			if !bytes.Equal(old, oldCopy) || !bytes.Equal(fresh, freshCopy) || !reflect.DeepEqual(allowed, allowedCopy) {
				t.Fatal("input mutated")
			}
		})
	}
}

func TestGoldenChild(t *testing.T) {
	path := os.Getenv("WIRE_TEST_GOLDEN")
	if path == "" {
		return
	}
	if os.Getenv("WIRE_TEST_READONLY") != "" {
		goldenWriterWithoutRoot(t)
	}
	current, err := os.ReadFile(os.Getenv("WIRE_TEST_CURRENT"))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	t.Cleanup(func() {
		if count != 1 {
			t.Errorf("render called %d times", count)
		}
	})
	var allowed []wire.AuthorizationAllowance
	if os.Getenv("WIRE_TEST_BAD_ALLOWANCE") != "" {
		allowed = []wire.AuthorizationAllowance{{}}
	}
	wire.GoldenWithAllowances(t, path, func() []byte { count++; return current }, allowed)
}

func runGolden(t *testing.T, old, current []byte, update, mode string) (string, []byte, error) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "golden.json")
	if mode != "missing" {
		if err := os.WriteFile(path, old, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "readonly" {
		if err := os.Chmod(path, 0o444); err != nil {
			t.Fatal(err)
		}
		// The child enters this directory before dropping root. Relative paths
		// then avoid traversing the parent's private test directories.
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "unreadable" {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	currentPath := filepath.Join(dir, "current.json")
	if err := os.WriteFile(currentPath, current, 0o644); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), executable, "-test.run=^TestGoldenChild$", "-test.v")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "WIRE_TEST_GOLDEN="+filepath.Base(path), "WIRE_TEST_CURRENT="+filepath.Base(currentPath), "UPDATE_GOLDEN="+update, "WIRE_TEST_BAD_ALLOWANCE=", "WIRE_TEST_READONLY=")
	if mode == "readonly" {
		cmd.Env = append(cmd.Env, "WIRE_TEST_READONLY=1")
	}
	if mode == "allowance" {
		cmd.Env = append(cmd.Env, "WIRE_TEST_BAD_ALLOWANCE=1")
	}
	output, err := cmd.CombinedOutput()
	onDisk, readErr := os.ReadFile(path)
	if mode == "missing" && !os.IsNotExist(readErr) {
		t.Fatalf("missing golden created: %v", readErr)
	}
	return string(output), onDisk, err
}

func TestGoldenRefusesEveryRuleBeforeWriting(t *testing.T) {
	for _, kind := range []string{"openapi", "asyncapi"} {
		for _, tc := range ruleCases(kind) {
			t.Run(kind+"/"+tc.name, func(t *testing.T) {
				old := baseline(kind)
				output, onDisk, err := runGolden(t, old, mutated(t, old, tc.change), "1", "")
				if err == nil || !strings.Contains(output, tc.message) || !strings.Contains(output, "is left as it was: the rules above stand, and UPDATE_GOLDEN=1 regenerates a document that is stale, never one that is broken") || !bytes.Equal(onDisk, old) {
					t.Fatalf("err=%v; unchanged=%v\n%s", err, bytes.Equal(onDisk, old), output)
				}
			})
		}
	}
}

func TestGoldenLifecycle(t *testing.T) {
	old := baseline("openapi")
	added := mutated(t, old, func(doc map[string]any) { doc["description"] = "additive prose" })
	for _, tc := range []struct {
		name, update, mode, message string
		current                     []byte
		pass, writes                bool
	}{
		{"equal", "", "", "", old, true, false},
		{"stale", "", "", "is stale; run with UPDATE_GOLDEN=1.\n        first difference at byte 2", added, false, false},
		{"additive update", "1", "", "rewrote", added, true, true},
		{"nonempty update", "yes", "", "rewrote", added, true, true},
		{"invalid equal", "1", "invalid", "document is invalid", []byte("null"), false, false},
		{"invalid current", "1", "", "current document is invalid", []byte("null"), false, false},
		{"invalid allowance", "1", "allowance", "invalid authorization allowance #0", added, false, false},
		{"missing", "", "missing", "(run with UPDATE_GOLDEN=1)", old, false, false},
		{"missing update", "1", "missing", "(run with UPDATE_GOLDEN=1)", old, false, false},
		{"unreadable", "1", "unreadable", "read ", old, false, false},
		{"write failure", "1", "readonly", "write ", added, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			golden := old
			if tc.mode == "invalid" {
				golden = tc.current
			}
			output, onDisk, err := runGolden(t, golden, tc.current, tc.update, tc.mode)
			if (err == nil) != tc.pass || !strings.Contains(output, tc.message) {
				t.Fatalf("err=%v\n%s", err, output)
			}
			if tc.mode == "missing" || tc.mode == "unreadable" {
				return
			}
			want := golden
			if tc.writes {
				want = tc.current
			}
			if !bytes.Equal(onDisk, want) {
				t.Fatal("unexpected golden bytes")
			}
		})
	}
}

func ExampleCompare() {
	golden := []byte(`{"openapi":"3.1.0","paths":{"/items":{"get":{"operationId":"list"}}}}`)
	current := []byte(`{"openapi":"3.1.0","paths":{}}`)
	for _, b := range wire.Compare(golden, current) {
		fmt.Println(b.Rule, b.Path, b.Member)
	}
	// Output:
	// B1 GET /items operationId
	// B2 GET /items route
}
