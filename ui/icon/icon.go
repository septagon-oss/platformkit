// Package icon is the glyph set the components draw with: the regular-weight
// Phosphor subset this application uses, vendored as path data.
//
// A glyph is a viewBox and a body, and Resolve is the whole interface. The
// version this is derived from had a provider chain with layer priorities, a
// registration seam with a restore function, and an XML sanitiser that walked
// each glyph checking element names and attribute allow-lists against a
// depth limit — machinery for glyphs arriving from somewhere else. Nothing
// arrives from somewhere else: every byte below is in this file, reviewed once,
// and a product that wants another glyph adds it here.
//
// Vector data is vendored from phosphor-icons/core under the MIT licence; see
// NOTICE. Package derived from github.com/septagon-oss/pk-ui (Apache-2.0).
package icon

import (
	"maps"
	"slices"
	"strings"
)

// ViewBox is the coordinate system every glyph here is drawn in. It is one
// value because a set with two would need each glyph to carry its own, and a
// mixed set is how icons end up at different optical sizes.
const ViewBox = "0 0 256 256"

// Glyph is one icon: its canonical name and the SVG elements inside the
// <svg> the Icon component draws. The body is trusted markup — it is a
// constant in this file — and the component writes it raw.
type Glyph struct {
	Name    string `json:"name"`
	Body    string `json:"body"`
	Source  string `json:"source"`
	License string `json:"license"`
}

// Fallback is the glyph an unknown name resolves to. A missing icon renders as
// a question mark rather than as nothing: a hole in a toolbar is a bug somebody
// notices, and an empty box is one they do not.
const Fallback = "question"

// Resolve is the glyph for a name, and whether the name was known. An alias
// resolves to what it names; anything else resolves to Fallback.
func Resolve(name string) (Glyph, bool) {
	canonical := strings.ToLower(strings.TrimSpace(name))
	canonical = strings.NewReplacer("_", "-", " ", "-").Replace(canonical)
	if to, ok := aliases[canonical]; ok {
		canonical = to
	}
	body, known := bodies[canonical]
	if !known {
		canonical, body = Fallback, bodies[Fallback]
	}
	source, license := "github.com/phosphor-icons/core", "MIT"
	if canonical == "sun" || canonical == "moon" {
		source, license = "github.com/septagon-oss/platformkit/ui/icon", "Apache-2.0"
	}
	return Glyph{Name: canonical, Body: body, Source: source, License: license}, known
}

// Names is every glyph this set draws, sorted. The gallery renders them all, so
// a glyph that is added and never used is visible rather than merely present.
func Names() []string {
	return slices.Sorted(maps.Keys(bodies))
}

// caret is the one chevron, turned. The four directions are one path drawn four
// ways rather than four paths that could disagree about their weight.
func caret(degrees string) string {
	if degrees == "0" {
		return caretDown
	}
	return `<g transform="rotate(` + degrees + ` 128 128)">` + caretDown + `</g>`
}

const caretDown = `<path d="M213.66,101.66l-80,80a8,8,0,0,1-11.32,0l-80-80A8,8,0,0,1,53.66,90.34L128,164.69l74.34-74.35a8,8,0,0,1,11.32,11.32Z"/>`

// aliases are the names other vocabularies use for these glyphs, so a caller
// that asks for "search" or "chevron-down" is not told there is no such icon.
var aliases = map[string]string{
	"alert":                  "warning",
	"chat-bubble-left-right": "chats",
	"chevron-down":           "caret-down",
	"cog":                    "gear",
	"document":               "file-text",
	"ellipsis-vertical":      "dots-three-vertical",
	"exclamation-circle":     "warning-circle",
	"exclamation-triangle":   "warning",
	"information-circle":     "info",
	"question-mark-circle":   "question",
	"search":                 "magnifying-glass",
	"upload":                 "upload-simple",
	"x-mark":                 "x",
}

var bodies = map[string]string{
	"caret-down":  caret("0"),
	"caret-up":    caret("180"),
	"caret-left":  caret("90"),
	"caret-right": caret("270"),

	// Two glyphs this set draws that Phosphor's regular weight is not the
	// source of: the theme toggle's pair, built from primitives rather than
	// vendored, because they are a circle with rays and a crescent.
	"sun":  `<circle cx="128" cy="128" r="52"/><path d="M128,36a8,8,0,0,1-8-8V16a8,8,0,0,1,16,0V28A8,8,0,0,1,128,36Zm0,184a8,8,0,0,0-8,8v12a8,8,0,0,0,16,0V228A8,8,0,0,0,128,220ZM228,120H216a8,8,0,0,0,0,16h12a8,8,0,0,0,0-16ZM40,128a8,8,0,0,0-8-8H20a8,8,0,0,0,0,16H32A8,8,0,0,0,40,128ZM60.4,49.1A8,8,0,0,0,49.1,60.4l8.5,8.5A8,8,0,0,0,68.9,57.6ZM187.1,187.1a8,8,0,0,0-11.3,11.3l8.5,8.5a8,8,0,0,0,11.3-11.3ZM195.6,49.1l-8.5,8.5a8,8,0,0,0,11.3,11.3l8.5-8.5A8,8,0,0,0,195.6,49.1ZM57.6,187.1l-8.5,8.5a8,8,0,0,0,11.3,11.3l8.5-8.5A8,8,0,0,0,57.6,187.1Z"/>`,
	"moon": `<path d="M224,152a96,96,0,0,1-120-120,8,8,0,0,0-10-10A104,104,0,1,0,234,162,8,8,0,0,0,224,152ZM128,216A88,88,0,0,1,86.7,50.5,104.06,104.06,0,0,0,205.5,169.3,88.05,88.05,0,0,1,128,216Z"/>`,

	"arrow-right":         `<path d="M221.66,133.66l-72,72a8,8,0,0,1-11.32-11.32L196.69,136H40a8,8,0,0,1,0-16H196.69L138.34,61.66a8,8,0,0,1,11.32-11.32l72,72A8,8,0,0,1,221.66,133.66Z"/>`,
	"calendar":            `<path d="M208,32H184V24a8,8,0,0,0-16,0v8H88V24a8,8,0,0,0-16,0v8H48A16,16,0,0,0,32,48V208a16,16,0,0,0,16,16H208a16,16,0,0,0,16-16V48A16,16,0,0,0,208,32ZM72,48v8a8,8,0,0,0,16,0V48h80v8a8,8,0,0,0,16,0V48h24V80H48V48ZM208,208H48V96H208V208Zm-96-88v64a8,8,0,0,1-16,0V132.94l-4.42,2.22a8,8,0,0,1-7.16-14.32l16-8A8,8,0,0,1,112,120Zm59.16,30.45L152,176h16a8,8,0,0,1,0,16H136a8,8,0,0,1-6.4-12.8l28.78-38.37A8,8,0,1,0,145.07,132a8,8,0,1,1-13.85-8A24,24,0,0,1,176,136,23.76,23.76,0,0,1,171.16,150.45Z"/>`,
	"chats":               `<path d="M216,80H184V48a16,16,0,0,0-16-16H40A16,16,0,0,0,24,48V176a8,8,0,0,0,13,6.22L72,154V184a16,16,0,0,0,16,16h93.59L219,230.22a8,8,0,0,0,5,1.78,8,8,0,0,0,8-8V96A16,16,0,0,0,216,80ZM66.55,137.78,40,159.25V48H168v88H71.58A8,8,0,0,0,66.55,137.78ZM216,207.25l-26.55-21.47a8,8,0,0,0-5-1.78H88V152h80a16,16,0,0,0,16-16V96h32Z"/>`,
	"check":               `<path d="M229.66,77.66l-128,128a8,8,0,0,1-11.32,0l-56-56a8,8,0,0,1,11.32-11.32L96,188.69,218.34,66.34a8,8,0,0,1,11.32,11.32Z"/>`,
	"check-circle":        `<path d="M173.66,98.34a8,8,0,0,1,0,11.32l-56,56a8,8,0,0,1-11.32,0l-24-24a8,8,0,0,1,11.32-11.32L112,148.69l50.34-50.35A8,8,0,0,1,173.66,98.34ZM232,128A104,104,0,1,1,128,24,104.11,104.11,0,0,1,232,128Zm-16,0a88,88,0,1,0-88,88A88.1,88.1,0,0,0,216,128Z"/>`,
	"clock":               `<path d="M128,24A104,104,0,1,0,232,128,104.11,104.11,0,0,0,128,24Zm0,192a88,88,0,1,1,88-88A88.1,88.1,0,0,1,128,216Zm64-88a8,8,0,0,1-8,8H128a8,8,0,0,1-8-8V72a8,8,0,0,1,16,0v48h48A8,8,0,0,1,192,128Z"/>`,
	"dots-three-vertical": `<path d="M140,128a12,12,0,1,1-12-12A12,12,0,0,1,140,128ZM128,72a12,12,0,1,0-12-12A12,12,0,0,0,128,72Zm0,112a12,12,0,1,0,12,12A12,12,0,0,0,128,184Z"/>`,
	"envelope":            `<path d="M224,48H32a8,8,0,0,0-8,8V192a16,16,0,0,0,16,16H216a16,16,0,0,0,16-16V56A8,8,0,0,0,224,48Zm-96,85.15L52.57,64H203.43ZM98.71,128,40,181.81V74.19Zm11.84,10.85,12,11.05a8,8,0,0,0,10.82,0l12-11.05,58,53.15H52.57ZM157.29,128,216,74.18V181.82Z"/>`,
	"file-text":           `<path d="M213.66,82.34l-56-56A8,8,0,0,0,152,24H56A16,16,0,0,0,40,40V216a16,16,0,0,0,16,16H200a16,16,0,0,0,16-16V88A8,8,0,0,0,213.66,82.34ZM160,51.31,188.69,80H160ZM200,216H56V40h88V88a8,8,0,0,0,8,8h48V216Zm-32-80a8,8,0,0,1-8,8H96a8,8,0,0,1,0-16h64A8,8,0,0,1,168,136Zm0,32a8,8,0,0,1-8,8H96a8,8,0,0,1,0-16h64A8,8,0,0,1,168,168Z"/>`,
	"gear":                `<path d="M128,80a48,48,0,1,0,48,48A48.05,48.05,0,0,0,128,80Zm0,80a32,32,0,1,1,32-32A32,32,0,0,1,128,160Zm88-29.84q.06-2.16,0-4.32l14.92-18.64a8,8,0,0,0,1.48-7.06,107.21,107.21,0,0,0-10.88-26.25,8,8,0,0,0-6-3.93l-23.72-2.64q-1.48-1.56-3-3L186,40.54a8,8,0,0,0-3.94-6,107.71,107.71,0,0,0-26.25-10.87,8,8,0,0,0-7.06,1.49L130.16,40Q128,40,125.84,40L107.2,25.11a8,8,0,0,0-7.06-1.48A107.6,107.6,0,0,0,73.89,34.51a8,8,0,0,0-3.93,6L67.32,64.27q-1.56,1.49-3,3L40.54,70a8,8,0,0,0-6,3.94,107.71,107.71,0,0,0-10.87,26.25,8,8,0,0,0,1.49,7.06L40,125.84Q40,128,40,130.16L25.11,148.8a8,8,0,0,0-1.48,7.06,107.21,107.21,0,0,0,10.88,26.25,8,8,0,0,0,6,3.93l23.72,2.64q1.49,1.56,3,3L70,215.46a8,8,0,0,0,3.94,6,107.71,107.71,0,0,0,26.25,10.87,8,8,0,0,0,7.06-1.49L125.84,216q2.16.06,4.32,0l18.64,14.92a8,8,0,0,0,7.06,1.48,107.21,107.21,0,0,0,26.25-10.88,8,8,0,0,0,3.93-6l2.64-23.72q1.56-1.48,3-3L215.46,186a8,8,0,0,0,6-3.94,107.71,107.71,0,0,0,10.87-26.25,8,8,0,0,0-1.49-7.06Zm-16.1-6.5a73.93,73.93,0,0,1,0,8.68,8,8,0,0,0,1.74,5.48l14.19,17.73a91.57,91.57,0,0,1-6.23,15L187,173.11a8,8,0,0,0-5.1,2.64,74.11,74.11,0,0,1-6.14,6.14,8,8,0,0,0-2.64,5.1l-2.51,22.58a91.32,91.32,0,0,1-15,6.23l-17.74-14.19a8,8,0,0,0-5-1.75h-.48a73.93,73.93,0,0,1-8.68,0,8,8,0,0,0-5.48,1.74L100.45,215.8a91.57,91.57,0,0,1-15-6.23L82.89,187a8,8,0,0,0-2.64-5.1,74.11,74.11,0,0,1-6.14-6.14,8,8,0,0,0-5.1-2.64L46.43,170.6a91.32,91.32,0,0,1-6.23-15l14.19-17.74a8,8,0,0,0,1.74-5.48,73.93,73.93,0,0,1,0-8.68,8,8,0,0,0-1.74-5.48L40.2,100.45a91.57,91.57,0,0,1,6.23-15L69,82.89a8,8,0,0,0,5.1-2.64,74.11,74.11,0,0,1,6.14-6.14A8,8,0,0,0,82.89,69L85.4,46.43a91.32,91.32,0,0,1,15-6.23l17.74,14.19a8,8,0,0,0,5.48,1.74,73.93,73.93,0,0,1,8.68,0,8,8,0,0,0,5.48-1.74L155.55,40.2a91.57,91.57,0,0,1,15,6.23L173.11,69a8,8,0,0,0,2.64,5.1,74.11,74.11,0,0,1,6.14,6.14,8,8,0,0,0,5.1,2.64l22.58,2.51a91.32,91.32,0,0,1,6.23,15l-14.19,17.74A8,8,0,0,0,199.87,123.66Z"/>`,
	"info":                `<path d="M128,24A104,104,0,1,0,232,128,104.11,104.11,0,0,0,128,24Zm0,192a88,88,0,1,1,88-88A88.1,88.1,0,0,1,128,216Zm16-40a8,8,0,0,1-8,8,16,16,0,0,1-16-16V128a8,8,0,0,1,0-16,16,16,0,0,1,16,16v40A8,8,0,0,1,144,176ZM112,84a12,12,0,1,1,12,12A12,12,0,0,1,112,84Z"/>`,
	"magnifying-glass":    `<path d="M229.66,218.34l-50.07-50.06a88.11,88.11,0,1,0-11.31,11.31l50.06,50.07a8,8,0,0,0,11.32-11.32ZM40,112a72,72,0,1,1,72,72A72.08,72.08,0,0,1,40,112Z"/>`,
	"plus":                `<path d="M224,128a8,8,0,0,1-8,8H136v80a8,8,0,0,1-16,0V136H40a8,8,0,0,1,0-16h80V40a8,8,0,0,1,16,0v80h80A8,8,0,0,1,224,128Z"/>`,
	"question":            `<path d="M140,180a12,12,0,1,1-12-12A12,12,0,0,1,140,180ZM128,72c-22.06,0-40,16.15-40,36v4a8,8,0,0,0,16,0v-4c0-11,10.77-20,24-20s24,9,24,20-10.77,20-24,20a8,8,0,0,0-8,8v8a8,8,0,0,0,16,0v-.72c18.24-3.35,32-17.9,32-35.28C168,88.15,150.06,72,128,72Zm104,56A104,104,0,1,1,128,24,104.11,104.11,0,0,1,232,128Zm-16,0a88,88,0,1,0-88,88A88.1,88.1,0,0,0,216,128Z"/>`,
	"trash":               `<path d="M216,48H176V40a24,24,0,0,0-24-24H104A24,24,0,0,0,80,40v8H40a8,8,0,0,0,0,16h8V208a16,16,0,0,0,16,16H192a16,16,0,0,0,16-16V64h8a8,8,0,0,0,0-16ZM96,40a8,8,0,0,1,8-8h48a8,8,0,0,1,8,8v8H96Zm96,168H64V64H192ZM112,104v64a8,8,0,0,1-16,0V104a8,8,0,0,1,16,0Zm48,0v64a8,8,0,0,1-16,0V104a8,8,0,0,1,16,0Z"/>`,
	"upload-simple":       `<path d="M224,144v64a8,8,0,0,1-8,8H40a8,8,0,0,1-8-8V144a8,8,0,0,1,16,0v56H208V144a8,8,0,0,1,16,0ZM93.66,77.66,120,51.31V144a8,8,0,0,0,16,0V51.31l26.34,26.35a8,8,0,0,0,11.32-11.32l-40-40a8,8,0,0,0-11.32,0l-40,40A8,8,0,0,0,93.66,77.66Z"/>`,
	"user":                `<path d="M230.92,212c-15.23-26.33-38.7-45.21-66.09-54.16a72,72,0,1,0-73.66,0C63.78,166.78,40.31,185.66,25.08,212a8,8,0,1,0,13.85,8c18.84-32.56,52.14-52,89.07-52s70.23,19.44,89.07,52a8,8,0,1,0,13.85-8ZM72,96a56,56,0,1,1,56,56A56.06,56.06,0,0,1,72,96Z"/>`,
	"warning":             `<path d="M236.8,188.09,149.35,36.22h0a24.76,24.76,0,0,0-42.7,0L19.2,188.09a23.51,23.51,0,0,0,0,23.72A24.35,24.35,0,0,0,40.55,224h174.9a24.35,24.35,0,0,0,21.33-12.19A23.51,23.51,0,0,0,236.8,188.09ZM222.93,203.8a8.5,8.5,0,0,1-7.48,4.2H40.55a8.5,8.5,0,0,1-7.48-4.2,7.59,7.59,0,0,1,0-7.72L120.52,44.21a8.75,8.75,0,0,1,15,0l87.45,151.87A7.59,7.59,0,0,1,222.93,203.8ZM120,144V104a8,8,0,0,1,16,0v40a8,8,0,0,1-16,0Zm20,36a12,12,0,1,1-12-12A12,12,0,0,1,140,180Z"/>`,
	"warning-circle":      `<path d="M128,24A104,104,0,1,0,232,128,104.11,104.11,0,0,0,128,24Zm0,192a88,88,0,1,1,88-88A88.1,88.1,0,0,1,128,216Zm-8-80V80a8,8,0,0,1,16,0v56a8,8,0,0,1-16,0Zm20,36a12,12,0,1,1-12-12A12,12,0,0,1,140,172Z"/>`,
	"x":                   `<path d="M205.66,194.34a8,8,0,0,1-11.32,11.32L128,139.31,61.66,205.66a8,8,0,0,1-11.32-11.32L116.69,128,50.34,61.66A8,8,0,0,1,61.66,50.34L128,116.69l66.34-66.35a8,8,0,0,1,11.32,11.32L139.31,128Z"/>`,
	"x-circle":            `<path d="M165.66,101.66,139.31,128l26.35,26.34a8,8,0,0,1-11.32,11.32L128,139.31l-26.34,26.35a8,8,0,0,1-11.32-11.32L116.69,128,90.34,101.66a8,8,0,0,1,11.32-11.32L128,116.69l26.34-26.35a8,8,0,0,1,11.32,11.32ZM232,128A104,104,0,1,1,128,24,104.11,104.11,0,0,1,232,128Zm-16,0a88,88,0,1,0-88,88A88.1,88.1,0,0,0,216,128Z"/>`,
}
