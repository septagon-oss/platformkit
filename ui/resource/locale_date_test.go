package resource

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

func TestDateValueFollowsTheRequestLocale(t *testing.T) {
	date := entity.Field{Name: "createdAt", Type: entity.TypeTime}
	value := "2026-10-02T13:05:00Z"
	copy := xtext.Load("en", xtext.Source{Name: "ui/resource", FS: Catalogues()})
	en := locale.SelectLocale(copy, "en")
	pt := locale.SelectLocale(copy, "pt-PT")
	english := fieldValue(Options{Locale: &en}, date, value)
	portuguese := fieldValue(Options{Locale: &pt}, date, value)
	if english == portuguese {
		t.Errorf("a date is displayed as %q in both en and pt-PT; the renderer ignores the request locale", portuguese)
	}
}
