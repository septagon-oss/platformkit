package rest

import (
	"regexp"
	"slices"
	"strconv"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// sketchCeilings are the figures a drawing enforces, as the value declares them, so a case can ask
// whether the words published beside the numeric bounds still name one of them.
var sketchCeilings = []int64{
	entity.SketchMaxStrokes, entity.SketchMaxPoints, entity.SketchMaxCoordinate, entity.SketchMaxMilliseconds,
}

// thousandsSeparator matches a grouping mark sitting between two digits — a comma, a full stop or a
// space — which a reader of the sentence ignores, so "20,000", "20.000" and "20 000" are one figure.
var thousandsSeparator = regexp.MustCompile(`([0-9])[,. ]+([0-9])`)

// digitRun matches one maximal run of digits in a sentence whose groups are folded.
var digitRun = regexp.MustCompile(`[0-9]+`)

// figuresIn lists each whole number the sentence states. Runs are maximal, so digits that merely
// begin with an enforced ceiling — "4000" beside an enforced 400 — are not read as that ceiling.
func figuresIn(sentence string) []string {
	for {
		folded := thousandsSeparator.ReplaceAllString(sentence, "$1$2")
		if folded == sentence {
			break
		}
		sentence = folded
	}
	return digitRun.FindAllString(sentence, -1)
}

// TestTheSketchSchemaWordsStateOnlyTheCeilingsTheValueDeclares reads a drawing field back off the
// wire schema the resource publishes and checks its two halves against the value. The numeric bounds
// are taken from entity.SketchMax*, so they have to carry the ceilings the value enforces; the
// description is prose typed beside them, so every whole number it states has to be a ceiling the
// value still declares, stated as a run of digits of its own, with the point ceiling among them
// because that bound is what the sentence exists to name. The figure zero is allowed unasked: the
// sentence names a stroke's start at zero milliseconds, which no constant holds. An API client — the
// phone among them — reads this schema as the contract, so words left describing a ceiling the value
// no longer enforces send it away with a body its own server then refuses.
func TestTheSketchSchemaWordsStateOnlyTheCeilingsTheValueDeclares(t *testing.T) {
	schema := &huma.Schema{Type: huma.TypeObject, Properties: map[string]*huma.Schema{}}
	annotateSketchFields(schema, []entity.Field{{Name: "drawing", Type: entity.TypeSketch, Required: true}})

	property := schema.Properties["drawing"]
	if property == nil || property.Items == nil || property.Items.Items == nil {
		t.Fatalf("the drawing field published no stroke schema: %#v", property)
	}
	if property.MaxItems == nil || *property.MaxItems != entity.SketchMaxStrokes {
		t.Errorf("the published stroke ceiling is %s, want the value's %d", whole(property.MaxItems), entity.SketchMaxStrokes)
	}
	stroke := property.Items
	if stroke.MaxItems == nil || *stroke.MaxItems != entity.SketchMaxPoints {
		t.Errorf("the published point ceiling is %s, want the value's %d", whole(stroke.MaxItems), entity.SketchMaxPoints)
	}
	prefix, _ := stroke.Items.Extensions["prefixItems"].([]*huma.Schema)
	if len(prefix) != 3 {
		t.Fatalf("the published point tuple names %d parts, want x, y and elapsed milliseconds", len(prefix))
	}
	if prefix[0].Maximum == nil || *prefix[0].Maximum != float64(entity.SketchMaxCoordinate) {
		t.Errorf("the published coordinate ceiling is %s, want the value's %d", decimal(prefix[0].Maximum), entity.SketchMaxCoordinate)
	}
	if prefix[2].Maximum == nil || *prefix[2].Maximum != float64(entity.SketchMaxMilliseconds) {
		t.Errorf("the published milliseconds ceiling is %s, want the value's %d", decimal(prefix[2].Maximum), entity.SketchMaxMilliseconds)
	}

	declared := map[string]bool{"0": true}
	for _, ceiling := range sketchCeilings {
		declared[strconv.FormatInt(ceiling, 10)] = true
	}
	stated := figuresIn(property.Description)
	for _, figure := range stated {
		if !declared[figure] {
			t.Errorf("the published drawing description %q states %s, which is no ceiling the drawing value declares", property.Description, figure)
		}
	}
	if !slices.Contains(stated, strconv.FormatInt(entity.SketchMaxPoints, 10)) {
		t.Errorf("the published drawing description %q names no figure of its own equal to the point ceiling %d", property.Description, entity.SketchMaxPoints)
	}
}

// whole renders an optional item count for a failure message.
func whole(count *int) string {
	if count == nil {
		return "unset"
	}
	return strconv.Itoa(*count)
}

// decimal renders an optional numeric bound for a failure message.
func decimal(bound *float64) string {
	if bound == nil {
		return "unset"
	}
	return strconv.FormatFloat(*bound, 'f', -1, 64)
}
