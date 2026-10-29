package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestResolvedSingletonOffersOnlyItsRecordAndUpdateSurfaces(t *testing.T) {
	r := resourceAt(t, "site", "settings")
	if !r.Singleton {
		t.Fatal("site settings is not registered as a singleton")
	}
	entry := screens.Describe1(r, true)
	if entry.Resolved == nil {
		t.Fatal("the site settings entry has no resolved policy")
	}
	if got := entry.Resolved.Layout.Columns; len(got) != 0 {
		t.Errorf("a singleton has no list, but resolved list columns are %v", got)
	}
	if got := entry.Resolved.Layout.Create; len(got) != 0 {
		t.Errorf("a singleton has no create route, but resolved create fields are %v", got)
	}
	if len(entry.Resolved.Layout.Update) == 0 {
		t.Error("the writable site settings singleton lost its update form")
	}
}
