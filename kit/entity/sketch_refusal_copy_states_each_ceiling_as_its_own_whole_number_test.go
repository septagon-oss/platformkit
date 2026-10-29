package entity_test

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// sketchCeilingRefusals carry each ceiling a drawing enforces beside the least drawing that breaks
// it, so a case can read the sentence the value answers with instead of trusting the copy written
// beside the constant.
var sketchCeilingRefusals = []struct {
	ceiling string
	value   int64
	tooMuch entity.Sketch
}{
	{"strokes", entity.SketchMaxStrokes, sketchWithStrokes(int(entity.SketchMaxStrokes) + 1)},
	{"points", entity.SketchMaxPoints, sketchWithPoints(int(entity.SketchMaxPoints) + 1)},
	{"coordinate", entity.SketchMaxCoordinate,
		entity.Sketch{{{0, 0, 0}, {entity.SketchMaxCoordinate + 1, 0, 1}}}},
	{"milliseconds", entity.SketchMaxMilliseconds,
		entity.Sketch{{{0, 0, 0}, {0, 0, entity.SketchMaxMilliseconds + 1}}}},
}

// sketchWithStrokes draws n one-point strokes, the least a drawing can be over the stroke ceiling.
func sketchWithStrokes(n int) entity.Sketch {
	sketch := make(entity.Sketch, n)
	for i := range sketch {
		sketch[i] = entity.SketchStroke{{0, 0, 0}}
	}
	return sketch
}

// sketchWithPoints draws one stroke of n points, the least a drawing can be over the point ceiling.
func sketchWithPoints(n int) entity.Sketch {
	stroke := make(entity.SketchStroke, n)
	for i := range stroke {
		stroke[i] = entity.SketchPoint{0, 0, 0}
	}
	return entity.Sketch{stroke}
}

// TestEverySketchRefusalStatesItsCeilingAsAWholeNumberOfItsOwn asks that the sentence the drawing
// value answers with when it refuses a limit states that limit as a figure standing on its own. A
// person told "at most 4000 strokes" by a field that refuses its four-hundred-and-first sends the
// same body again, or keeps a board every writing door refuses, and digits that merely begin with
// the enforced bound are read as the bound. Thousands grouping is folded first, because "20,000" and
// "20 000" are one figure to the reader. Only the value's own sentences are read here: the editor's
// copy of them and each language catalogue's are read where those are rendered.
func TestEverySketchRefusalStatesItsCeilingAsAWholeNumberOfItsOwn(t *testing.T) {
	for _, ceiling := range sketchCeilingRefusals {
		err := ceiling.tooMuch.Validate()
		if err == nil {
			t.Errorf("a drawing one %s over %d was accepted, want the refusal whose copy is checked", ceiling.ceiling, ceiling.value)
			continue
		}
		figure := strconv.FormatInt(ceiling.value, 10)
		if !statesFigureAlone(foldDigitGroups(err.Error()), figure) {
			t.Errorf("the %s refusal says %q, which states no number of its own equal to the ceiling %s", ceiling.ceiling, err.Error(), figure)
		}
	}
}

// digitGroup matches a thousands separator sitting between two digits: a comma, a full stop or a
// space, which a reader of the sentence ignores.
var digitGroup = regexp.MustCompile(`([0-9])[,. ]+([0-9])`)

// foldDigitGroups removes the separators between digits, repeatedly, so one figure may be written
// either way in the copy.
func foldDigitGroups(sentence string) string {
	for {
		folded := digitGroup.ReplaceAllString(sentence, "$1$2")
		if folded == sentence {
			return folded
		}
		sentence = folded
	}
}

// digitsRun matches one maximal run of digits in a sentence whose groups are folded.
var digitsRun = regexp.MustCompile(`[0-9]+`)

// statesFigureAlone reports whether sentence states figure as a run of digits of its own, rather
// than as the leading digits of a longer one.
func statesFigureAlone(sentence, figure string) bool {
	for _, run := range digitsRun.FindAllString(sentence, -1) {
		if run == figure {
			return true
		}
	}
	return false
}
