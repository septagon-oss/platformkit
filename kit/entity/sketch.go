package entity

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strconv"
)

// SketchPoint is [x, y, elapsed milliseconds] in a normalized square.
type SketchPoint [3]int64

// SketchStroke is an ordered, nonempty sequence whose elapsed time starts at zero.
type SketchStroke []SketchPoint

// Sketch is an ordered drawing. Nil means unset; a non-nil empty slice means
// explicitly empty. Assignment aliases slices; callers own synchronization.
type Sketch []SketchStroke

// Strokes returns a deep copy of validated [x, y, elapsed milliseconds] tuples,
// preserving unset versus empty. Invalid values return nil and ErrInvalidSketch.
func (s Sketch) Strokes() ([][][3]int64, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, nil
	}
	strokes := make([][][3]int64, len(s))
	for i, stroke := range s {
		strokes[i] = make([][3]int64, len(stroke))
		for j, point := range stroke {
			strokes[i][j] = point
		}
	}
	return strokes, nil
}

const (
	TypeSketch            FieldType = "sketch"
	SketchMaxStrokes                = 400
	SketchMaxPoints                 = 20000
	SketchMaxCoordinate             = 1000
	SketchMaxMilliseconds           = 9007199254740991
)

// ErrInvalidSketch identifies malformed drawings across JSON and SQL boundaries.
var ErrInvalidSketch = errors.New("invalid sketch")

type sketchError string

func (e sketchError) Error() string { return string(e) }
func (e sketchError) Unwrap() error { return ErrInvalidSketch }

const (
	sketchShape   sketchError = "Use strokes of [x, y, ms] whole numbers."
	sketchStrokes sketchError = "A drawing can contain at most 400 strokes. Undo or clear some strokes and try again."
	sketchPoints  sketchError = "A drawing can contain at most 20,000 points. Undo or clear some strokes and try again."
)

// Validate checks shape, aggregate caps, coordinates and per-stroke elapsed time
// without changing the drawing. Both unset and explicitly empty values are valid.
func (s Sketch) Validate() error {
	if len(s) > SketchMaxStrokes {
		return sketchStrokes
	}
	points := 0
	for _, stroke := range s {
		points += len(stroke)
	}
	if points > SketchMaxPoints {
		return sketchPoints
	}
	for _, stroke := range s {
		if len(stroke) == 0 {
			return sketchError("Each stroke needs at least one point.")
		}
		var previous int64
		for i, point := range stroke {
			if err := validateSketchPoint(point, previous, i == 0); err != nil {
				return err
			}
			previous = point[2]
		}
	}
	return nil
}

func validateSketchPoint(point SketchPoint, previous int64, first bool) error {
	if point[0] < 0 || point[0] > SketchMaxCoordinate || point[1] < 0 || point[1] > SketchMaxCoordinate {
		return sketchError("Keep drawing coordinates between 0 and 1000.")
	}
	if point[2] < 0 || point[2] > SketchMaxMilliseconds {
		return sketchError("Use elapsed milliseconds between 0 and 9007199254740991.")
	}
	if first && point[2] != 0 || !first && point[2] < previous {
		return sketchError("Start each stroke at 0 milliseconds and keep its times in order.")
	}
	return nil
}

// MarshalJSON validates before encoding and preserves unset versus empty.
func (s Sketch) MarshalJSON() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal([]SketchStroke(s))
}

// UnmarshalJSON accepts only integer tuples and replaces the receiver after
// complete validation. Failure, including a nil receiver, leaves it unchanged.
func (s *Sketch) UnmarshalJSON(data []byte) error {
	if s == nil {
		return sketchError("A drawing needs a destination.")
	}
	data = bytes.TrimSpace(data)
	if !json.Valid(data) {
		return sketchShape
	}
	if bytes.Equal(data, []byte("null")) {
		*s = nil
		return nil
	}
	if data[0] != '[' {
		return sketchShape
	}
	strokes, points := 0, 0
	sketchElements(data, func(stroke []byte) bool {
		strokes++
		if stroke[0] == '[' {
			sketchElements(stroke, func([]byte) bool { points++; return true })
		}
		return true
	})
	if strokes > SketchMaxStrokes {
		return sketchStrokes
	}
	if points > SketchMaxPoints {
		return sketchPoints
	}
	out := make(Sketch, 0, strokes)
	var refused error
	sketchElements(data, func(raw []byte) bool {
		var stroke SketchStroke
		stroke, refused = decodeSketchStroke(raw)
		if refused != nil {
			return false
		}
		out = append(out, stroke)
		return true
	})
	if refused != nil {
		return refused
	}
	if err := out.Validate(); err != nil {
		return err
	}
	*s = out
	return nil
}

func decodeSketchStroke(raw []byte) (SketchStroke, error) {
	if raw[0] != '[' {
		return nil, sketchShape
	}
	stroke := make(SketchStroke, 0)
	var refused error
	sketchElements(raw, func(tuple []byte) bool {
		if tuple[0] != '[' {
			refused = sketchShape
			return false
		}
		var point SketchPoint
		n := 0
		sketchElements(tuple, func(number []byte) bool {
			if n == 3 {
				refused = sketchShape
				return false
			}
			v, err := strconv.ParseInt(string(number), 10, 64)
			if err != nil {
				refused = sketchShape
				return false
			}
			point[n] = v
			n++
			return true
		})
		if n != 3 {
			refused = sketchShape
		}
		if refused != nil {
			return false
		}
		previous := int64(0)
		if len(stroke) > 0 {
			previous = stroke[len(stroke)-1][2]
		}
		if refused = validateSketchPoint(point, previous, len(stroke) == 0); refused != nil {
			return false
		}
		stroke = append(stroke, point)
		return true
	})
	if refused == nil && len(stroke) == 0 {
		refused = sketchError("Each stroke needs at least one point.")
	}
	return stroke, refused
}

// sketchElements visits slices of an already syntax-checked JSON array. Counting
// before decoding bounds tuple storage without allocating a generic JSON tree.
func sketchElements(data []byte, visit func([]byte) bool) {
	start, depth, quoted, escaped := 1, 0, false, false
	for i := 1; i < len(data); i++ {
		c := data[i]
		if quoted {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if depth == 0 && (c == ',' || i == len(data)-1) {
			element := bytes.TrimSpace(data[start:i])
			if len(element) > 0 && !visit(element) {
				return
			}
			start = i + 1
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		}
	}
}

// Value returns validated JSON for a JSONB column, or SQL NULL for unset.
func (s Sketch) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	return s.MarshalJSON()
}

// Scan accepts SQL NULL, string or bytes. Invalid input preserves the receiver;
// successful decoding owns its storage and retains no driver buffer.
func (s *Sketch) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		return s.UnmarshalJSON([]byte("null"))
	case []byte:
		return s.UnmarshalJSON(v)
	case string:
		return s.UnmarshalJSON([]byte(v))
	default:
		return sketchError("Read a drawing from JSON or SQL NULL.")
	}
}
