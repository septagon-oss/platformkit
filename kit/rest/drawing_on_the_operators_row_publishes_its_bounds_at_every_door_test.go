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

// operatorsDrawingSettings is a tenant's one row held behind the installation's own grants: the shape
// rest.Singleton serves when OperatorRead and OperatorWrite move both doors to the operator's surface.
// The drawing sits on the row itself, which is where a module keeps a drawing the tenant configures once.
type operatorsDrawingSettings struct {
	crud.Base
	Drawing  entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Revision int64         `json:"revision" readOnly:"true"`
}

func (operatorsDrawingSettings) TableName() string { return "rest_operators_boards" }

// operatorDocument is the served OpenAPI document as this question is asked of it: the named components,
// and for each operation the surface the kernel wrote it on and the media types it offers for a body or
// an answer.
type operatorDocument struct {
	Components struct {
		Schemas map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
	Paths map[string]map[string]struct {
		Surface     string `json:"x-platformkit-surface"`
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

// servedOperatorDocument mounts a tenant's drawing-carrying row on the operator's surface and returns the
// document a client generates from. The mount is unwired: only /openapi.json is asked, nothing answers.
func servedOperatorDocument(t *testing.T) operatorDocument {
	t.Helper()
	board := rest.Singleton[*operatorsDrawingSettings]{
		Module: "drawing", Entity: "board", Path: "/board",
		Read: "drawing:read", Write: "drawing:write",
		OperatorRead: true, OperatorWrite: true,
		Load: func(context.Context, db.Tx[db.Tenant]) (*operatorsDrawingSettings, error) {
			return &operatorsDrawingSettings{}, nil
		},
		Save: func(_ context.Context, _ db.Tx[db.Tenant], in *operatorsDrawingSettings) (*operatorsDrawingSettings, error) {
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
	var document operatorDocument
	if err := json.Unmarshal(w.Body.Bytes(), &document); err != nil {
		t.Fatalf("the served document is not the JSON a generator reads: %v (%s)", err, w.Body)
	}
	return document
}

// publishedDrawing returns the drawing one content schema publishes, following a $ref to the named object.
func publishedDrawing(document operatorDocument, content map[string]any) map[string]any {
	schema := content
	if ref, _ := content["$ref"].(string); ref != "" {
		named, held := document.Components.Schemas[strings.TrimPrefix(ref, "#/components/schemas/")]
		if !held {
			return nil
		}
		schema = map[string]any{"properties": named.Properties}
	}
	properties, _ := schema["properties"].(map[string]any)
	field, _ := properties["drawing"].(map[string]any)
	return field
}

// TestADrawingOnTheOperatorsRowPublishesItsBoundsAtEveryDoor asks the operator's own doors of a tenant's
// one drawing row. Both answer at the installation host rather than the tenant's, and whatever the
// document offers of the row at any media type is what a person generates their types from, so the bounds
// have to be in each of them. Every representation is asked for the ceilings the value holds — at most its
// count of strokes, at most its count of points per stroke, a point of exactly three integers each floored
// at 0 and capped at the ceiling the value carries. A raw upload is the only content allowed to carry no
// drawing, so the case cannot pass by finding nothing, and each door answers for itself.
func TestADrawingOnTheOperatorsRowPublishesItsBoundsAtEveryDoor(t *testing.T) {
	document := servedOperatorDocument(t)
	var bodies, answers int
	for path, operations := range document.Paths {
		for method, operation := range operations {
			if operation.Surface != "ops" {
				t.Errorf("%s %s is served on surface %q, want the operator's own", path, strings.ToUpper(method), operation.Surface)
			}
			if operation.RequestBody != nil {
				for media, content := range operation.RequestBody.Content {
					if media == "application/octet-stream" {
						continue
					}
					published := publishedDrawing(document, content.Schema)
					if published == nil {
						t.Errorf("%s PUT body %s offers the row without a drawing at all", path, media)
						continue
					}
					askOperatorDoor(t, path+" PUT body "+media, published)
					bodies++
				}
			}
			for status, answer := range operation.Responses {
				if !strings.HasPrefix(status, "2") {
					continue
				}
				for media, content := range answer.Content {
					published := publishedDrawing(document, content.Schema)
					if published == nil {
						t.Errorf("%s %s answers %s %s with no drawing in the row it offers", path, strings.ToUpper(method), status, media)
						continue
					}
					askOperatorDoor(t, path+" "+strings.ToUpper(method)+" answers "+status+" "+media, published)
					answers++
				}
			}
		}
	}
	if bodies == 0 {
		t.Error("no request body of the served document publishes the drawing, so nothing here was asked of a write")
	}
	if answers == 0 {
		t.Error("no answer of the served document publishes the drawing, so nothing here was asked of a read")
	}
}

// askOperatorDoor asks one published drawing for the bounds the value refuses to cross. Every question is
// asked without stopping at the first that fails to hold: a door whose point is missing altogether must
// still leave the other doors asked, or one broken door would report alone and the rest pass unseen.
func askOperatorDoor(t *testing.T, where string, published map[string]any) {
	t.Helper()
	stroke, _ := published["items"].(map[string]any)
	point, _ := stroke["items"].(map[string]any)
	if point == nil {
		t.Errorf("%s publishes no point array inside its drawing: %v", where, published)
		return
	}
	if bound, held := published["maxItems"].(float64); !held || int64(bound) != entity.SketchMaxStrokes {
		t.Errorf("%s publishes a drawing ceiling of %v, want the value's %d strokes", where, published["maxItems"], entity.SketchMaxStrokes)
	}
	if bound, held := stroke["maxItems"].(float64); !held || int64(bound) != entity.SketchMaxPoints {
		t.Errorf("%s publishes a stroke ceiling of %v, want the value's %d points", where, stroke["maxItems"], entity.SketchMaxPoints)
	}
	parts, _ := point["prefixItems"].([]any)
	ceilings := []int64{entity.SketchMaxCoordinate, entity.SketchMaxCoordinate, entity.SketchMaxMilliseconds}
	if len(parts) != len(ceilings) {
		t.Errorf("%s names %d parts of a point in the served bytes, want the %d the value parses", where, len(parts), len(ceilings))
	}
	if bound, held := point["maxItems"].(float64); !held || int(bound) != len(ceilings) {
		t.Errorf("%s publishes a point of %v numbers, want exactly the %d the value parses", where, point["maxItems"], len(ceilings))
	}
	for part, ceiling := range ceilings {
		if len(parts) <= part {
			continue
		}
		named, _ := parts[part].(map[string]any)
		if named == nil {
			t.Errorf("%s publishes point part %d as %#v, want a schema of its own", where, part, parts[part])
			continue
		}
		if named["type"] != "integer" {
			t.Errorf("%s publishes the %dth number of a point as %v, want an integer", where, part, named["type"])
		}
		if bound, held := named["minimum"].(float64); !held || bound != 0 {
			t.Errorf("%s publishes no floor of 0 for the %dth number of a point", where, part)
		}
		if bound, held := named["maximum"].(float64); !held || int64(bound) != ceiling {
			t.Errorf("%s publishes a ceiling of %v for the %dth number of a point, want the value's %d", where, named["maximum"], part, ceiling)
		}
	}
}
