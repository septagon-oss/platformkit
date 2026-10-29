package rest

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
)

func MergeIntegerFixture(value any, fields []crud.Field, patch map[string]any) error {
	_, err := merge(value, fields, nil, patch)
	return err
}

func CheckIntegerFixtureRevision(tag string, revision int64) error {
	return refuseStaleTag(tag, revision)
}

func TestMapBindingRefusesAnAlreadyRoundedInteger(t *testing.T) {
	for _, value := range []any{float64(9007199254740993), float64(-9007199254740993), float32(16777217), float32(-16777217)} {
		row := &patchMergeRow{Count: 7}
		if _, err := merge(row, crud.Fields[*patchMergeRow](), nil, map[string]any{"count": value}); err == nil || row.Count != 7 {
			t.Fatalf("accepted uncertain float %v: %+v %v", value, row, err)
		}
	}
	for _, value := range []any{json.Number("9007199254740993"), "9007199254740993"} {
		row := new(patchMergeRow)
		if _, err := merge(row, crud.Fields[*patchMergeRow](), nil, map[string]any{"count": value}); err != nil || row.Count != 9007199254740993 {
			t.Fatalf("lost exact input %v: %+v %v", value, row, err)
		}
	}
}
