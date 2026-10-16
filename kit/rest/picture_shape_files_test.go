package rest

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// pictureShapes holds one column of every Go shape a picture name may sit on.
type pictureShapes struct {
	crud.Base
	Cover      uuid.UUID    `json:"cover" ui:"present:image"`
	Avatar     *uuid.UUID   `json:"avatar" ui:"present:avatar"`
	Photos     []uuid.UUID  `json:"photos" ui:"present:images"`
	MaybeShots *[]uuid.UUID `json:"maybeShots" ui:"present:images"`
}

func (pictureShapes) TableName() string { return "picture_shapes" }

// TestEveryPictureShapeTheGateAdmitsFilesItsIDs: a column the mount gate lets
// carry a picture name has every id it holds handed to the file ledger. A shape
// the gate admits and the ledger reads as empty is a picture the release sweep
// is entitled to delete while a record still shows it.
func TestEveryPictureShapeTheGateAdmitsFilesItsIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	shots := []uuid.UUID{a, b}
	row := &pictureShapes{Cover: a, Avatar: &a, Photos: shots, MaybeShots: &shots}
	want := map[string][]uuid.UUID{
		"cover": {a}, "avatar": {a}, "photos": {a, b}, "maybeShots": {a, b},
	}
	value := reflect.ValueOf(row).Elem()
	for _, f := range crud.Fields[*pictureShapes]() {
		ids, ok := want[f.Name]
		if !ok {
			continue
		}
		// A shape the gate refuses never reaches a write; only an admitted one
		// owes the ledger its ids.
		if pictureFault(f) != "" {
			continue
		}
		got := pictureIDs(value.FieldByIndex(f.Index))
		if !reflect.DeepEqual(got, ids) {
			t.Errorf("field %q: the ledger is handed %v, want %v", f.Name, got, ids)
		}
	}
}
