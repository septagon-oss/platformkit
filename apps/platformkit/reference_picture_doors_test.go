package main

import (
	"testing"

	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
)

// TestTheReferenceCompositionDeclaresNoComposedPictureDoor checks the doors the
// reference application's own sentence hands the generated screens: the file
// module's stored-picture door, and no composed-picture door, because this
// application composes no picture module to answer one. A door named here with
// nothing mounted behind it is a broken image on every generated list.
func TestTheReferenceCompositionDeclaresNoComposedPictureDoor(t *testing.T) {
	_, cfg := configure(t)
	c := compose(cfg)
	doors := value[admincontracts.Pictures](c.planned)
	if doors.Source != filecontracts.PictureSource {
		t.Errorf("stored-picture door = %q, want %q", doors.Source, filecontracts.PictureSource)
	}
	if doors.ComposedSource != "" {
		t.Errorf("composed-picture door = %q, want none", doors.ComposedSource)
	}
	for _, m := range c.modules {
		if m.Name == "picture" {
			t.Errorf("the reference composition composes the picture module")
		}
	}
}
