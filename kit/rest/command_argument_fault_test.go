package rest

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

func TestCommandArgumentOwnershipUsesMetadataAndFieldOrder(t *testing.T) {
	fields := []crud.Field{
		{Name: "condition", Type: entity.TypeInt},
		{Name: "server\"note", Type: entity.TypeString, ReadOnly: true},
		{Name: "revision", Type: entity.TypeInt},
	}
	want := `argument field "server\"note" is server-owned; use a caller-owned argument name instead (ifRevision for an observed revision)`
	for range 3 {
		if got := commandFault("confirm", entity.CommandHints{}, fields); got != want {
			t.Errorf("fault = %q, want %q", got, want)
		}
	}
}

func TestCommandArgumentOwnershipPreservesDiagnosticPrecedence(t *testing.T) {
	fields := []crud.Field{{Name: "revision", Type: entity.TypeInt, Presentation: entity.FieldHints{Format: "not-a-format"}}}
	for _, tc := range []struct {
		name, verb string
		present    entity.CommandHints
		want       string
	}{
		{"verb", "Confirm task", entity.CommandHints{}, `command "Confirm task" is not a path segment`},
		{"presentation", "confirm", entity.CommandHints{Primary: true, Destructive: true}, `command "confirm" is both primary and destructive; a screen cannot offer a button as the expected action and as the one that hurts`},
		{"ownership before field hints", "confirm", entity.CommandHints{}, `argument field "revision" is server-owned; use a caller-owned argument name instead (ifRevision for an observed revision)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := commandFault(tc.verb, tc.present, fields); got != tc.want {
				t.Errorf("fault = %q, want %q", got, tc.want)
			}
		})
	}
	fields[0].Name = "ifRevision"
	if got, want := commandFault("confirm", entity.CommandHints{}, fields), fieldHintFault("confirm argument", fields, entity.EntryHints{}); got == "" || got != want {
		t.Errorf("caller-owned field hint fault = %q, want %q", got, want)
	}
}
