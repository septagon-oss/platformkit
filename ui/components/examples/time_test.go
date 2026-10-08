package examples_test

import (
	"strings"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestStandardTimeCaptureKeepsItsStringSchemaAndValue(t *testing.T) {
	value := c.TimeText{AtUTC: time.Date(2026, 9, 29, 12, 0, 0, 123, time.UTC), Text: "29 September"}
	e := examples.ExampleOf(examples.ExampleInfo{ID: "time", ComponentID: "time"}, value, func(p c.TimeText) g.Node { return g.Text(p.AtUTC.Format(time.RFC3339Nano)) })
	d, err := e.Describe()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(d.Schema), `"format":"date-time"`) || !strings.Contains(string(d.Props), `"2026-09-29T12:00:00.000000123Z"`) {
		t.Fatalf("time codec drift: %s %s", d.Schema, d.Props)
	}
	code, err := e.GoProps()
	if err != nil || !strings.Contains(code, "time.Date(2026, time.Month(9), 29, 12, 0, 0, 123, time.UTC)") {
		t.Fatalf("copyable time lost its value: %s %v", code, err)
	}
	changed, err := e.WithProps([]byte(`{"atUTC":"2026-10-01T09:00:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	next, err := changed.Describe()
	if err != nil || !strings.Contains(next.HTML, "2026-10-01T09:00:00Z") {
		t.Fatalf("typed timestamp edit failed: %v", err)
	}
	if strings.Contains(d.HTML, "2026-10-01") {
		t.Fatal("timestamp edit mutated the source capture")
	}
}
