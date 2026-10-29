package rest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// servedDrawing is one place the API document publishes a drawing: an operation's request body, or
// one of its answers, at the media type it is served under. A field published as present-or-null is
// followed into the branch carrying the strokes, so a bound held in one branch only is still read.
type servedDrawing struct {
	where     string
	published map[string]any
	writes    bool // a body a client sends, rather than an answer it reads
	integers  bool // this very object publishes the row's revision as decimal text
}

// drawingsInTheServedDocument mounts the drawing at every door that publishes one — the read answer,
// the create and patch bodies, each in its decimal-string variant, and a command's own request body —
// and returns every drawing the document names with where it was published. A command's body is the
// door written out where it is used rather than named as a component, so it is reached through the
// operation that offers it.
func drawingsInTheServedDocument(t *testing.T) []servedDrawing {
	t.Helper()
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	api, router := patchDocumentAPI()
	s.Mount(api.Surfaces(s.Module))
	rest.Command(api.Surfaces(s.Module), s, "replace", "Replace drawing", "", []string{s.Event(rest.Updated)},
		func(context.Context, db.Tx[db.Tenant], uuid.UUID, sketchReplacement) (*SketchRow, error) {
			return nil, nil
		},
		rest.CommandOptions{})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("the served document = %d %s", w.Code, w.Body.String())
	}
	var document struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]any `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
			RequestBody *struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"requestBody"`
			Responses map[string]struct {
				Content map[string]struct {
					Schema map[string]any `json:"schema"`
				} `json:"content"`
			} `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatalf("the served document is not the JSON a generator reads: %v (%s)", err, w.Body)
	}
	var doors []servedDrawing
	read := func(where string, writes bool, content map[string]any) {
		schema := content
		if ref, _ := content["$ref"].(string); ref != "" {
			named, held := document.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
			if !held || named.Properties == nil {
				return
			}
			schema = map[string]any{"properties": named.Properties}
		}
		properties, _ := schema["properties"].(map[string]any)
		published, held := drawingBranch(properties["drawing"])
		if !held {
			return
		}
		revision, _ := properties["revision"].(map[string]any)
		doors = append(doors, servedDrawing{where: where, published: published, writes: writes, integers: revision["type"] == "string"})
	}
	for path, operations := range document.Paths {
		for method, operation := range operations {
			if operation.RequestBody == nil {
				continue
			}
			for media, content := range operation.RequestBody.Content {
				read(path+" "+method+" body "+media, true, content.Schema)
			}
			for status, answer := range operation.Responses {
				for media, content := range answer.Content {
					read(operation.OperationID+" answers "+status+" "+media, false, content.Schema)
				}
			}
		}
	}
	return doors
}

// drawingBranch returns the drawing one object publishes, following a union to the branch that carries the
// strokes.
func drawingBranch(field any) (map[string]any, bool) {
	if field == nil {
		return nil, false
	}
	published, _ := field.(map[string]any)
	branches, isUnion := published["anyOf"].([]any)
	if !isUnion {
		return published, published != nil
	}
	for _, candidate := range branches {
		branch, _ := candidate.(map[string]any)
		if _, hasStrokes := branch["items"]; hasStrokes {
			return branch, true
		}
	}
	return nil, false
}

// TestEveryDoorOfTheDocumentPublishesTheDrawingBoundsTheValueEnforces asks the bounds of a drawing of
// the document a client generates its types from, door by door, because a schema built for one field
// answers for none of them: the answer publishes the row, the patch republishes the field as a union
// with null, and a command's body is written out where it is offered. The kernel rewrites that whole
// document afterwards so a row's own 64-bit counts travel as decimal text, and a drawing sits inside
// the objects that rewrite touches. So each door is asked for the tuple the value parses — three
// parts, each an integer, floored where the value refuses to go below it, each capped at the ceiling
// the value holds — and for the ceilings over it. A bound reaching one door only leaves a phone
// generating a drawing it cannot send to the rest.
func TestEveryDoorOfTheDocumentPublishesTheDrawingBoundsTheValueEnforces(t *testing.T) {
	ceilings := []int64{entity.SketchMaxCoordinate, entity.SketchMaxCoordinate, entity.SketchMaxMilliseconds}
	var writes, answers, rewritten int
	for _, door := range drawingsInTheServedDocument(t) {
		stroke, _ := door.published["items"].(map[string]any)
		point, _ := stroke["items"].(map[string]any)
		if point == nil {
			t.Fatalf("%s publishes no point array inside its drawing: %v", door.where, door.published["items"])
		}
		if bound, held := door.published["maxItems"].(float64); !held || int64(bound) != entity.SketchMaxStrokes {
			t.Errorf("%s publishes a drawing ceiling of %v, want the value's %d strokes", door.where, door.published["maxItems"], entity.SketchMaxStrokes)
		}
		if bound, held := stroke["maxItems"].(float64); !held || int64(bound) != entity.SketchMaxPoints {
			t.Errorf("%s publishes a stroke ceiling of %v, want the value's %d points", door.where, stroke["maxItems"], entity.SketchMaxPoints)
		}
		low, hasLow := point["minItems"].(float64)
		high, hasHigh := point["maxItems"].(float64)
		if !hasLow || !hasHigh || int(low) != len(ceilings) || int(high) != len(ceilings) {
			t.Errorf("%s publishes a point of %v to %v numbers, want exactly the %d the value parses", door.where, point["minItems"], point["maxItems"], len(ceilings))
		}
		if element, _ := point["items"].(map[string]any); element["type"] != "integer" {
			t.Errorf("%s publishes the numbers of a point as %v, want integers the way the value refuses every other number", door.where, element["type"])
		}
		parts, _ := point["prefixItems"].([]any)
		if len(parts) != len(ceilings) {
			t.Fatalf("%s names %d parts of a point in the served bytes, want %d", door.where, len(parts), len(ceilings))
		}
		for part, ceiling := range ceilings {
			named, _ := parts[part].(map[string]any)
			if named == nil {
				t.Fatalf("%s publishes point part %d as %#v, want a schema of its own", door.where, part, parts[part])
			}
			if named["type"] != "integer" {
				t.Errorf("%s publishes the %dth number of a point as %v, want an integer the way the value refuses every other number", door.where, part, named["type"])
			}
			if bound, held := named["minimum"].(float64); !held || bound != 0 {
				t.Errorf("%s publishes no floor of 0 for the %dth number of a point", door.where, part)
			}
			if bound, held := named["maximum"].(float64); !held || int64(bound) != ceiling {
				t.Errorf("%s publishes a ceiling of %v for the %dth number of a point, want the value's %d", door.where, named["maximum"], part, ceiling)
			}
		}
		if door.writes {
			writes++
		} else {
			answers++
		}
		if door.integers {
			rewritten++
		}
	}
	if writes == 0 {
		t.Error("no request body of the served document publishes a drawing, so nothing here was asked of a write")
	}
	if answers == 0 {
		t.Error("no answer of the served document publishes a drawing, so nothing here was asked of a read")
	}
	if rewritten == 0 {
		t.Error("no published drawing sits in an object whose own revision is written as decimal text, so nothing here asked the tuple of the rewritten answer")
	}
}
