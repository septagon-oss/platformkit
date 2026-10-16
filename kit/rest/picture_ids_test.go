package rest

import (
	"reflect"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestPictureIDsPreservesNullableAndNamedLists(t *testing.T) {
	type photos []uuid.UUID
	a, b := uuid.New(), uuid.New()
	for _, tc := range []struct {
		name  string
		value any
		want  []uuid.UUID
	}{
		{"absent id", (*uuid.UUID)(nil), nil},
		{"absent list", (*[]uuid.UUID)(nil), nil},
		{"empty list", new([]uuid.UUID(nil)), nil},
		{"zero id", new(uuid.Nil), nil},
		{"named list", photos{a, uuid.Nil, b}, []uuid.UUID{a, b}},
		{"nullable named list", new(photos{a, uuid.Nil, b}), []uuid.UUID{a, b}},
		{"nullable elements", []*uuid.UUID{nil, &a, new(uuid.Nil), &b}, []uuid.UUID{a, b}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := pictureIDs(reflect.ValueOf(tc.value)); !slices.Equal(got, tc.want) {
				t.Errorf("picture IDs = %v, want %v", got, tc.want)
			}
		})
	}
}
