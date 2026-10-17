package picture_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/picture"
	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestPictureCompositionRequiresSourceAndValidFiniteDeclarations(t *testing.T) {
	template, err := contracts.Compile("fixture", picturetest.SVG)
	if err != nil {
		t.Fatal(err)
	}
	provider := func(templates contracts.Templates) *pkit.Module {
		return pkit.NewModule("fixture", func(w *pkit.Wiring) (module.Module, error) {
			pkit.Put[contracts.Source](w, &picturetest.Fake{})
			pkit.Put[contracts.Templates](w, templates)
			return module.Module{Name: "fixture"}, nil
		}, pkit.Provides[contracts.Source](), pkit.Provides[contracts.Templates]())
	}
	deployment := pkit.Deployment{Environment: pkit.Development}
	if err := pkit.NewApp("pictures").Use(picture.Module).Validate(deployment); err == nil || !strings.Contains(err.Error(), "Source") {
		t.Fatalf("missing Source %v", err)
	}
	if err := pkit.NewApp("pictures").Use(provider(contracts.Templates{{}}), picture.Module).Validate(deployment); err == nil {
		t.Fatal("zero template accepted")
	}
	if err := pkit.NewApp("pictures").Use(provider(contracts.Templates{template, template}), picture.Module).Validate(deployment); err != nil {
		t.Fatal(err)
	}
	var declarations contracts.Templates
	for i := range 65 {
		source := picturetest.SVG + strings.Repeat(" ", i)
		entry, err := contracts.Compile("fixture", source)
		if err != nil {
			t.Fatal(err)
		}
		declarations = append(declarations, entry)
	}
	if err := pkit.NewApp("pictures").Use(provider(declarations), picture.Module).Validate(deployment); err == nil {
		t.Fatal("more than 64 declarations accepted")
	}
}
