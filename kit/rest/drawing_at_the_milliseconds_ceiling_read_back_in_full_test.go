package rest_test

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestADrawingAtTheMillisecondsCeilingReadsBackEveryDigitItSent asks the reading
// door the question the writing doors cannot answer: a board saved with a point at
// the field's own largest legal elapsed time is served with the digits it was
// sent, and a reader that decodes that answer into a generic JSON tree — the shape
// a browser's JSON.parse, a gateway's map and any re-encoder build, where every
// number is a double — still names that same number. The patch door then refuses
// the next millisecond, so the range a person may store is exactly the range a
// generic reader can hand back.
//
// The field's ceiling is the largest integer a double writes exactly, and that is
// why it is that number and not the top of an int64: the numbers of a drawing sit
// inside a tuple the field encodes itself, so the wire profile that carries an
// exact int64 as a string reaches none of them. Widen the ceiling and every Go
// reader stays honest while the browser redraws and re-posts a time nobody drew,
// and saving what the person was shown becomes either a different drawing or a
// refusal of a row they never changed.
func TestADrawingAtTheMillisecondsCeilingReadsBackEveryDigitItSent(t *testing.T) {
	const ceiling = int64(entity.SketchMaxMilliseconds)
	atCeiling := `[[[0,0,0],[7,7,` + strconv.FormatInt(ceiling, 10) + `]]]`
	s := rest.Spec[*SketchRow]{Module: "drawing", Entity: "drawing", Path: "/drawings", Read: "drawing:read", Write: "drawing:write"}
	_, router, admin := mountAs(t, s, hosts{map[string]tenancy.Tenant{host: acme}})
	if _, err := admin.ExecContext(t.Context(), sketchDDL); err != nil {
		t.Fatal(err)
	}
	code, body := call(t, router, http.MethodPost, "/api/v1/drawing/drawings", `{"title":"Board","drawing":`+atCeiling+`}`)
	if code != http.StatusCreated {
		t.Fatalf("create the board at the ceiling = %d %s", code, body)
	}
	at := "/api/v1/drawing/drawings/" + id(t, body)

	code, answer := call(t, router, http.MethodGet, at, "")
	if code != http.StatusOK {
		t.Fatalf("the read of the board at the ceiling = %d %s", code, answer)
	}
	var served struct {
		Drawing json.RawMessage `json:"drawing"`
	}
	if err := json.Unmarshal([]byte(answer), &served); err != nil {
		t.Fatalf("the read is not a problem: %v (%s)", err, answer)
	}
	if string(served.Drawing) != atCeiling {
		t.Errorf("the read served %s, want the digits it was sent, %s", served.Drawing, atCeiling)
	}

	// The worst honest reader: the answer decoded into numbers a double holds, as
	// a browser and any re-encoding intermediary see it.
	var tree map[string]any
	if err := json.Unmarshal([]byte(answer), &tree); err != nil {
		t.Fatalf("the read does not decode as a generic JSON tree: %v (%s)", err, answer)
	}
	strokes, ok := tree["drawing"].([]any)
	if !ok || len(strokes) != 1 {
		t.Fatalf("the tree holds no drawing of one stroke: %s", answer)
	}
	points, ok := strokes[0].([]any)
	if !ok || len(points) != 2 {
		t.Fatalf("the tree holds no stroke of two points: %s", answer)
	}
	point, ok := points[1].([]any)
	if !ok || len(point) != 3 {
		t.Fatalf("the tree holds no [x, y, ms] point: %s", answer)
	}
	elapsed, ok := point[2].(float64)
	if !ok {
		t.Fatalf("a generic reader does not read the elapsed time as a number: %#v", point[2])
	}
	if int64(elapsed) != ceiling {
		t.Errorf("a generic reader of the served drawing reads the field's largest legal elapsed time as %d, not %d: a browser would show and re-post a time nobody drew", int64(elapsed), ceiling)
	}

	// The same question asked of the engine that draws it, as kit/rest already asks
	// of an exact money amount: JavaScript reads every JSON number as a double, so
	// the digits it prints are the ones a person would see on the board and post
	// back again.
	const script = `const v=JSON.parse(require('fs').readFileSync(0,'utf8'));process.stdout.write(String(v.drawing[0][1][2]))`
	read := exec.CommandContext(t.Context(), "node", "-e", script)
	read.Stdin = strings.NewReader(answer)
	shown, err := read.CombinedOutput()
	if err != nil {
		t.Fatalf("javascript reads the served drawing: %v %s", err, shown)
	}
	if string(shown) != strconv.FormatInt(ceiling, 10) {
		t.Errorf("javascript reads the field's largest legal elapsed time as %s, want %d: the board a person is shown would not be the board they saved", shown, ceiling)
	}

	// The range ends where a generic reader stops being exact: the next
	// millisecond is nobody's to store, at the door and not only in the value.
	next := `[[[0,0,0],[7,7,` + strconv.FormatInt(ceiling+1, 10) + `]]]`
	if code, answer := call(t, router, http.MethodPatch, at, `{"drawing":`+next+`}`); code != http.StatusUnprocessableEntity {
		t.Errorf("the json patch door accepted the millisecond past the ceiling (%d %s), want the refusal that keeps the stored range exactly readable", code, answer)
	}
}
