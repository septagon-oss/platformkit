package main

import (
	"encoding/json"
	"os"
	"testing"
)

// TestTheOpenAPIGoldenNamesEveryPictureAndRowHint checks that the checked-in
// device contract describes each hint a client reads to draw a picture, finish a
// status or swipe a row, on the schema that carries it: a regenerated golden
// that dropped one would leave a native shell with no field to read.
func TestTheOpenAPIGoldenNamesEveryPictureAndRowHint(t *testing.T) {
	raw, err := os.ReadFile(openapiGolden)
	if err != nil {
		t.Fatalf("read %s: %v", openapiGolden, err)
	}
	var doc struct {
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", openapiGolden, err)
	}
	want := map[string][]string{
		"EntryPresentation": {"coverField", "thumbnailField", "primaryCommand"},
		"FieldHints":        {"doneValues"},
	}
	for schema, names := range want {
		s, ok := doc.Components.Schemas[schema]
		if !ok {
			t.Errorf("%s has no schema %s", openapiGolden, schema)
			continue
		}
		for _, name := range names {
			if _, ok := s.Properties[name]; !ok {
				t.Errorf("%s: schema %s does not describe %q", openapiGolden, schema, name)
			}
		}
	}
}
