package rest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
)

// pointCeilings are the ceilings over the three numbers of a point, in the order the value parses them.
var pointCeilings = []int64{entity.SketchMaxCoordinate, entity.SketchMaxCoordinate, entity.SketchMaxMilliseconds}

// singletonSketchBoard is a tenant's one board, the shape rest.Singleton serves and rest.Spec does not:
// no id in the path, so the doors a drawing reaches are the row's answer and the PUT body that writes it.
type singletonSketchBoard struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (singletonSketchBoard) TableName() string { return "rest_singleton_boards" }

// singletonDocument is the served OpenAPI document of a mount whose drawing sits on a tenant's one row:
// the named components, and the operations that offer a body or an answer.
type singletonDocument struct {
	Components struct {
		Schemas map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
	Paths map[string]map[string]struct {
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

// servedSingletonDocument mounts a tenant's one drawing-carrying row and returns the document a client
// generates its types from. The mount is unwired: nothing here answers a request, only the document is asked.
func servedSingletonDocument(t *testing.T) singletonDocument {
	t.Helper()
	board := rest.Singleton[*singletonSketchBoard]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write",
		Load: func(context.Context, db.Tx[db.Tenant]) (*singletonSketchBoard, error) {
			return &singletonSketchBoard{}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *singletonSketchBoard) (*singletonSketchBoard, error) {
			return in, nil
		},
	}
	api, router := patchDocumentAPI()
	board.Mount(api.Surfaces(board.Module))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("the served document = %d %s", w.Code, w.Body.String())
	}
	var document singletonDocument
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatalf("the served document is not the JSON a generator reads: %v (%s)", err, w.Body)
	}
	return document
}

// publishedSingletonDrawing returns the drawing one content schema publishes, following a $ref to the
// named object and a union to the branch that carries the strokes.
func publishedSingletonDrawing(document singletonDocument, content map[string]any) (map[string]any, bool) {
	schema := content
	if ref, _ := content["$ref"].(string); ref != "" {
		named, held := document.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
		if !held {
			return nil, false
		}
		schema = map[string]any{"properties": named.Properties}
	}
	properties, _ := schema["properties"].(map[string]any)
	field, _ := properties["drawing"].(map[string]any)
	if field == nil {
		return nil, false
	}
	branches, isUnion := field["anyOf"].([]any)
	if !isUnion {
		return field, true
	}
	for _, candidate := range branches {
		branch, _ := candidate.(map[string]any)
		if _, hasStrokes := branch["items"]; hasStrokes {
			return branch, true
		}
	}
	return nil, false
}

// TestTheSingletonDoorPublishesTheDrawingBoundsTheValueEnforces asks the bounds of a drawing of the
// document served for a tenant's one row. One annotator puts those bounds into every door, and a module
// whose drawing sits on the tenant's settings rather than in a collection reaches a client through the
// singleton's own two: the answer to GET and the body of the PUT that writes it. A bound that stopped
// reaching those would leave a phone generating a drawing type with no bounds at all for that module, and
// nothing else in the document would say so, because the singleton answers at a path no component is named
// after. Each door is asked for what the value itself holds: a drawing at most the value's count of strokes,
// a stroke at most its count of points, and a point of exactly three integers floored at 0 and each capped
// at the ceiling the value carries. It refuses to pass by finding nothing, and asks one door of each kind.
func TestTheSingletonDoorPublishesTheDrawingBoundsTheValueEnforces(t *testing.T) {
	document := servedSingletonDocument(t)
	var writes, answers int
	for path, operations := range document.Paths {
		for method, operation := range operations {
			if operation.RequestBody != nil {
				for media, content := range operation.RequestBody.Content {
					if door, held := publishedSingletonDrawing(document, content.Schema); held {
						checkSingletonDrawingDoor(t, path+" "+strings.ToUpper(method)+" body "+media, door)
						writes++
					}
				}
			}
			for status, answer := range operation.Responses {
				for media, content := range answer.Content {
					if door, held := publishedSingletonDrawing(document, content.Schema); held {
						checkSingletonDrawingDoor(t, path+" answers "+status+" "+media, door)
						answers++
					}
				}
			}
		}
	}
	if writes == 0 {
		t.Error("no request body of the served singleton document publishes a drawing, so nothing here was asked of a write")
	}
	if answers == 0 {
		t.Error("no answer of the served singleton document publishes a drawing, so nothing here was asked of a read")
	}
}

// checkSingletonDrawingDoor asks one published drawing for the bounds the value refuses to cross.
func checkSingletonDrawingDoor(t *testing.T, where string, published map[string]any) {
	t.Helper()
	stroke, _ := published["items"].(map[string]any)
	point, _ := stroke["items"].(map[string]any)
	if point == nil {
		t.Fatalf("%s publishes no point array inside its drawing: %v", where, published["items"])
	}
	if bound, held := published["maxItems"].(float64); !held || int64(bound) != entity.SketchMaxStrokes {
		t.Errorf("%s publishes a drawing ceiling of %v, want the value's %d strokes", where, published["maxItems"], entity.SketchMaxStrokes)
	}
	if bound, held := stroke["maxItems"].(float64); !held || int64(bound) != entity.SketchMaxPoints {
		t.Errorf("%s publishes a stroke ceiling of %v, want the value's %d points", where, stroke["maxItems"], entity.SketchMaxPoints)
	}
	low, hasLow := point["minItems"].(float64)
	high, hasHigh := point["maxItems"].(float64)
	if !hasLow || !hasHigh || int(low) != len(pointCeilings) || int(high) != len(pointCeilings) {
		t.Errorf("%s publishes a point of %v to %v numbers, want exactly the %d the value parses", where, point["minItems"], point["maxItems"], len(pointCeilings))
	}
	parts, _ := point["prefixItems"].([]any)
	if len(parts) != len(pointCeilings) {
		t.Fatalf("%s names %d parts of a point in the served bytes, want %d", where, len(parts), len(pointCeilings))
	}
	for part, ceiling := range pointCeilings {
		named, _ := parts[part].(map[string]any)
		if named == nil {
			t.Fatalf("%s publishes point part %d as %#v, want a schema of its own", where, part, parts[part])
		}
		if named["type"] != "integer" {
			t.Errorf("%s publishes the %dth number of a point as %v, want an integer the way the value refuses every other number", where, part, named["type"])
		}
		if bound, held := named["minimum"].(float64); !held || bound != 0 {
			t.Errorf("%s publishes no floor of 0 for the %dth number of a point", where, part)
		}
		if bound, held := named["maximum"].(float64); !held || int64(bound) != ceiling {
			t.Errorf("%s publishes a ceiling of %v for the %dth number of a point, want the value's %d", where, named["maximum"], part, ceiling)
		}
	}
}
