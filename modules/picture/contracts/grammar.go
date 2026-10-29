package contracts

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	namePattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	colourPattern  = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	numberPattern  = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?`)
	xmlDeclaration = regexp.MustCompile(`^version\s*=\s*(?:"1\.0"|'1\.0')(?:\s+encoding\s*=\s*(?:"[Uu][Tt][Ff]-8"|'[Uu][Tt][Ff]-8'))?(?:\s+standalone\s*=\s*(?:"yes"|'yes'))?\s*$`)
)

func validName(s string) bool { return namePattern.MatchString(s) }
func trim(s string) string    { return strings.Trim(s, " \t\r\n") }
func decimal(n float64) string {
	if n == 0 {
		return "0"
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

// numericList checks separators before parsing so compact adjacent numbers cannot
// acquire different meanings in validation and SVG rendering.
func numericList(s string, budget *int) ([]float64, error) {
	s = trim(s)
	var out []float64
	for s != "" {
		spelling := numberPattern.FindString(s)
		if spelling == "" {
			return nil, errors.New("invalid numeric-list syntax")
		}
		n, err := strconv.ParseFloat(spelling, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1000000 {
			return nil, errors.New("numbers must be finite and at most 1000000")
		}
		if budget != nil {
			*budget++
			if *budget > 16384 {
				return nil, errors.New("maximum 16384 numeric tokens")
			}
		}
		out = append(out, n)
		rest := s[len(spelling):]
		s = trim(rest)
		if s == "" {
			break
		}
		if strings.HasPrefix(s, ",") {
			s = trim(s[1:])
			if s == "" {
				return nil, errors.New("trailing comma in numeric list")
			}
		} else if len(rest) == len(s) {
			return nil, errors.New("numeric-list separator is required")
		}
	}
	return out, nil
}
func validateElement(e *element, root bool, budget *int, figure bool) error {
	specific := map[string]string{"svg": "xmlns viewBox width height preserveAspectRatio", "g": "", "path": "d", "rect": "x y width height rx ry", "circle": "cx cy r", "ellipse": "cx cy rx ry", "line": "x1 y1 x2 y2", "polyline": "points", "polygon": "points"}
	if figure {
		specific["text"] = "x y font-size text-anchor font-weight"
	}
	attrs, ok := specific[e.name]
	if !ok || (!root && e.name == "svg") {
		return fmt.Errorf("element %s is forbidden", e.name)
	}
	if !root {
		attrs += " fill stroke stroke-width stroke-linecap stroke-linejoin stroke-miterlimit fill-rule opacity fill-opacity stroke-opacity transform"
		if figure {
			attrs += " data-choice"
		} else {
			attrs += " data-part data-option data-slot"
		}
	}
	for k, v := range e.attrs {
		if !strings.Contains(" "+attrs+" ", " "+k+" ") {
			return fmt.Errorf("attribute %s is forbidden", k)
		}
		if err := attribute(e, k, v, root, budget); err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
	}
	return requiredAttributes(e, root)
}

func attribute(e *element, k, v string, root bool, budget *int) error {
	var err error
	switch k {
	case "xmlns", "data-part", "data-option", "data-slot", "data-choice":
	case "fill", "stroke":
		if v != "none" && !colourPattern.MatchString(v) {
			err = errors.New("paint must be none or #RRGGBB")
		}
	case "text-anchor":
		err = vocabulary(v, "start middle end")
	case "font-weight":
		err = vocabulary(v, "normal bold 100 200 300 400 500 600 700 800 900")
	case "font-size":
		ns, fault := numericList(v, budget)
		err = fault
		if err == nil && (len(ns) != 1 || ns[0] <= 0 || ns[0] > 256) {
			err = errors.New("font-size must be positive and at most 256")
		}
	case "stroke-linecap":
		err = vocabulary(v, "butt round square")
	case "stroke-linejoin":
		err = vocabulary(v, "miter round bevel")
	case "fill-rule":
		err = vocabulary(v, "nonzero evenodd")
	case "preserveAspectRatio":
		if v != "none" && v != "xMidYMid meet" && v != "xMidYMid slice" {
			err = errors.New("unsupported preserveAspectRatio")
		}
	case "transform":
		err = transform(v, budget)
	case "d":
		err = path(v, budget)
	default:
		err = numericAttribute(e, k, v, root, budget)
	}
	return err
}

func numericAttribute(e *element, k, v string, root bool, budget *int) error {

	ns, err := numericList(v, budget)
	if err == nil {
		switch k {
		case "viewBox":
			if len(ns) != 4 || ns[2] <= 0 || ns[3] <= 0 || ns[2] > 8192 || ns[3] > 8192 {
				err = errors.New("viewBox needs four numbers and positive axes at most 8192")
			}
		case "points":
			minimum := 4
			if e.name == "polygon" {
				minimum = 6
			}
			if len(ns) < minimum || len(ns)%2 != 0 {
				err = errors.New("points needs complete coordinate pairs")
			}
		default:
			if len(ns) != 1 {
				err = errors.New("one number is required")
				break
			}
			lo, hi := -1000000.0, 1000000.0
			switch k {
			case "opacity", "fill-opacity", "stroke-opacity":
				lo, hi = 0, 1
			case "stroke-width":
				lo, hi = 0, 8192
			case "stroke-miterlimit":
				lo, hi = 1, 1000
			case "width", "height", "rx", "ry", "r":
				lo = 0
			}
			if ns[0] < lo || ns[0] > hi {
				err = errors.New("number outside the permitted interval")
			}
			if root && (k == "width" || k == "height") && (ns[0] <= 0 || ns[0] > 8192) {
				err = errors.New("dimension must be positive and at most 8192")
			}
		}
	}
	return err
}

func requiredAttributes(e *element, root bool) error {
	required := map[string]string{"svg": "viewBox", "path": "d", "rect": "width height", "circle": "r", "ellipse": "rx ry", "polyline": "points", "polygon": "points"}
	for k := range strings.FieldsSeq(required[e.name]) {
		if _, ok := e.attrs[k]; !ok {
			return fmt.Errorf("required attribute %s is absent", k)
		}
	}
	if root {
		ns, err := numericList(e.attrs["viewBox"], nil)
		if err != nil || len(ns) != 4 {
			return errors.New("required viewBox is invalid")
		}
		for i, k := range []string{"width", "height"} {
			if _, ok := e.attrs[k]; !ok {
				e.attrs[k] = decimal(ns[i+2])
			}
		}
		if _, ok := e.attrs["preserveAspectRatio"]; !ok {
			e.attrs["preserveAspectRatio"] = "xMidYMid meet"
		}
	}
	return nil
}
func vocabulary(s, words string) error {
	if !strings.Contains(" "+words+" ", " "+s+" ") || s == "" {
		return errors.New("unsupported vocabulary")
	}
	return nil
}
func transform(s string, budget *int) error {
	s = trim(s)
	if s == "" {
		return errors.New("empty transform")
	}
	for s != "" {
		at := strings.IndexByte(s, '(')
		if at < 0 {
			return errors.New("transform requires arguments")
		}
		name := s[:at]
		end := strings.IndexByte(s, ')')
		if end < at {
			return errors.New("unclosed transform")
		}
		ns, err := numericList(s[at+1:end], budget)
		if err != nil {
			return err
		}
		n := len(ns)
		good := false
		switch name {
		case "matrix":
			good = n == 6
		case "translate", "scale":
			good = n == 1 || n == 2
		case "rotate":
			good = n == 1 || n == 3
		case "skewX", "skewY":
			good = n == 1
		}
		if !good {
			return errors.New("unsupported transform or argument arity")
		}
		rest := s[end+1:]
		s = trim(rest)
		if s != "" && len(s) == len(rest) {
			return errors.New("transforms require whitespace")
		}
	}
	return nil
}
func path(s string, budget *int) error {
	s = trim(s)
	if s == "" || (s[0] != 'M' && s[0] != 'm') {
		return errors.New("path must begin with M")
	}
	arities := map[byte]int{'M': 2, 'L': 2, 'H': 1, 'V': 1, 'C': 6, 'S': 4, 'Q': 4, 'T': 2, 'A': 7, 'Z': 0}
	for s != "" {
		command := s[0]
		upper := command
		if upper >= 'a' && upper <= 'z' {
			upper -= 32
		}
		arity, ok := arities[upper]
		if !ok {
			return errors.New("unknown path command")
		}
		s = s[1:]
		end := 0
		for end < len(s) {
			b := s[end]
			if (b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z') && b != 'e' && b != 'E' {
				break
			}
			end++
		}
		args := trim(s[:end])
		if strings.HasPrefix(args, ",") || strings.HasSuffix(args, ",") {
			return errors.New("comma next to path command")
		}
		ns, err := numericList(args, budget)
		if err != nil {
			return err
		}
		if arity == 0 {
			if len(ns) != 0 {
				return errors.New("Z takes no arguments")
			}
		} else if len(ns) == 0 || len(ns)%arity != 0 {
			return errors.New("incomplete path argument group")
		}
		if upper == 'A' {
			// Flags are literal tokens, not merely numbers equal to zero or one.
			tokens := strings.Fields(strings.ReplaceAll(args, ",", " "))
			for i := 0; i < len(ns); i += 7 {
				if ns[i] < 0 || ns[i+1] < 0 || (tokens[i+3] != "0" && tokens[i+3] != "1") || (tokens[i+4] != "0" && tokens[i+4] != "1") {
					return errors.New("invalid arc radius or flag")
				}
			}
		}
		s = trim(s[end:])
	}
	return nil
}
