package rest

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

type derivedOnly struct {
	crud.Base
	Picture entity.Picture `json:"picture" readOnly:"true" ui:"present:image"`
}

func (derivedOnly) TableName() string { return "derived_only" }

type noPictureUses struct{ t *testing.T }

func (f noPictureUses) SetUses(context.Context, db.Tx[db.Tenant], UsesInput) error {
	f.t.Fatal("derived picture reached stored-file uses")
	return nil
}
func TestDerivedPicturesNeverReachFileUses(t *testing.T) {
	p, _ := entity.ComposedPicture("source")
	row := &derivedOnly{Picture: p}
	s := Spec[*derivedOnly]{FileUses: noPictureUses{t}}
	if err := s.prepareFiles(t.Context(), db.Tx[db.Tenant]{}, row, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.clearFileUses(t.Context(), db.Tx[db.Tenant]{}, row); err != nil {
		t.Fatal(err)
	}
	f, ok := entity.FieldNamed(crud.Fields[*derivedOnly](), "picture")
	if !ok {
		t.Fatal("picture field missing")
	}
	for _, change := range []func(*crud.Field){func(f *crud.Field) { f.ReadOnly = false }, func(f *crud.Field) { f.Widget = "text" }, func(f *crud.Field) { f.Present = "avatar" }, func(f *crud.Field) { f.Present = "images" }, func(f *crud.Field) { f.Presentation.Format = "date" }} {
		copy := f
		change(&copy)
		if err := pictureFault(copy); err == "" {
			t.Errorf("invalid declaration admitted %+v", copy)
		}
	}
}
