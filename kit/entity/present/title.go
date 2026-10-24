package present

import (
	"strings"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

// Title names a row: the first candidate field whose value is not blank, else
// id when it is not blank, else empty. The caller passes Titles.ListCandidates
// for a list and Titles.RecordCandidates for a record.
func Title(candidates []string, row map[string]any, id, empty string) string {
	for _, name := range candidates {
		if v := strings.TrimSpace(display.Text(row[name])); v != "" {
			return v
		}
	}
	if v := strings.TrimSpace(id); v != "" {
		return v
	}
	return empty
}

// Ago is the signed distance from now to at as whole units, truncated toward
// zero: unit is second (under a minute), minute (under an hour), hour (under a
// day) or day; n is negative for the past. The words are the client's.
func Ago(at, now time.Time) (unit string, n int) {
	d := at.Sub(now)
	abs := d.Abs()
	switch {
	case abs < time.Minute:
		return "second", int(d / time.Second)
	case abs < time.Hour:
		return "minute", int(d / time.Minute)
	case abs < 24*time.Hour:
		return "hour", int(d / time.Hour)
	}
	return "day", int(d / (24 * time.Hour))
}

func fieldNamed(fields []entity.Field, name string) (entity.Field, bool) {
	return entity.FieldNamed(fields, name)
}
