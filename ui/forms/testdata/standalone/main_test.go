package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/modules/task/domain"
)

func TestIndependentBlocksComposeWithoutApplicationRuntime(t *testing.T) {
	fields := entity.Fields[*note]()
	if len(fields) != 8 || !fields[0].ReadOnly {
		t.Fatalf("entity metadata = %#v", fields)
	}
	fields[3].Name = "mutated"
	if entity.Fields[*note]()[3].Name != "title" {
		t.Fatal("entity metadata is shared mutable state")
	}
	a := newApplication()
	if got := locale.SelectLocale(a.messages, "pt-PT").Text("save", "Save"); got != "Guardar" {
		t.Fatalf("locale = %q", got)
	}
	form := a.form("second-editor")
	description, err := form.Describe()
	if err != nil || description.ID != "external/second-editor" || !strings.Contains(description.HTML, "Guardar") {
		t.Fatalf("localized form: %v", err)
	}
	if len(description.Children) != 6 {
		t.Fatalf("form children = %d", len(description.Children))
	}
	change, err := domain.Resolve(domain.StatusOpen, "", " repaired ")
	if err != nil || !change.Changed || change.Text != "repaired" {
		t.Fatalf("decision = %#v, %v", change, err)
	}
	retry, err := domain.Resolve(domain.StatusResolved, change.Text, "")
	if err != nil || retry.Changed {
		t.Fatalf("retry = %#v, %v", retry, err)
	}
	if _, err := domain.Resolve(domain.StatusClosed, "", "done"); !errors.Is(err, domain.ErrClosed) {
		t.Fatal("closed refusal lost")
	}
}
