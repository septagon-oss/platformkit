package rest

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
)

type patchMergeRow struct {
	crud.Base
	Title    string     `json:"title"`
	State    string     `json:"state" enum:"open,done" default:"open"`
	Count    int64      `json:"count" default:"7"`
	Enabled  bool       `json:"enabled" default:"true"`
	ReadOnly string     `json:"readOnly" readOnly:"true"`
	DueAt    *time.Time `json:"dueAt,omitempty"`
	Choices  []string   `json:"choices,omitempty"`
	Owned    string     `json:"owned"`
	Hidden   string     `json:"-"`
}

func (patchMergeRow) TableName() string { return "patch_merge_rows" }

func TestPatchProjectionValuesUseTheExistingMerge(t *testing.T) {
	fields := crud.Fields[*patchMergeRow]()
	immutable := []string{"owned"}
	values := map[string]any{"title": "new", "state": "done", "count": float64(0), "enabled": false, "readOnly": "writable", "dueAt": nil, "choices": []string{"a", "b"}}
	for _, field := range fields {
		t.Run(field.Name, func(t *testing.T) {
			row := &patchMergeRow{Title: "before", State: "done", Count: 9, Enabled: true, DueAt: new(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)), Choices: []string{"before"}}
			before := *row
			value, writable := values[field.Name]
			columns, err := merge(row, fields, immutable, map[string]any{field.Name: value})
			if !writable {
				reason := field.Name + " is read-only"
				if field.Name == "owned" {
					reason = "owned belongs to a route of its own"
				}
				if !errors.Is(err, crud.ErrInvalid) || !strings.Contains(err.Error(), reason) {
					t.Errorf("refusal = %v, want %q", err, reason)
				}
				if !reflect.DeepEqual(*row, before) {
					t.Error("refused one-field merge changed row")
				}
				return
			}
			if err != nil || !reflect.DeepEqual(columns, []string{field.Column}) {
				t.Fatalf("columns %v, error %v", columns, err)
			}
			previous, current := reflect.ValueOf(before), reflect.ValueOf(*row)
			for _, omitted := range fields {
				if omitted.Name != field.Name && !reflect.DeepEqual(previous.FieldByIndex(omitted.Index).Interface(), current.FieldByIndex(omitted.Index).Interface()) {
					t.Errorf("merge changed omitted %s", omitted.Name)
				}
			}
		})
	}
	for _, key := range []string{"hidden", "Hidden", "Title", "unknown"} {
		_, err := merge(&patchMergeRow{}, fields, immutable, map[string]any{key: nil})
		if !errors.Is(err, crud.ErrInvalid) || !strings.Contains(err.Error(), "there is no field") {
			t.Errorf("%s = %v", key, err)
		}
	}
	row := &patchMergeRow{Title: "before", Count: 9, State: "done", Enabled: true}
	before := *row
	columns, err := merge(row, fields, immutable, map[string]any{})
	if err != nil || len(columns) != 0 || !reflect.DeepEqual(*row, before) {
		t.Errorf("empty merge = %+v, %v, %v", row, columns, err)
	}
}
