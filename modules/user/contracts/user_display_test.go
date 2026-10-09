package contracts_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// A person's row is called by their name: the generated list's link, the record's heading and the
// browser tab read the field the entity marks `ui:"display"`, and without the mark a screen falls back
// to an order that can end at the id. One mark, on displayName, and on nothing else.
func TestAPersonIsCalledByTheirName(t *testing.T) {
	var marked []string
	for _, f := range entity.FieldsOf(reflect.TypeFor[contracts.User]()) {
		if f.Display {
			marked = append(marked, f.Name)
		}
	}
	if len(marked) != 1 || marked[0] != "displayName" {
		t.Fatalf("a user's row should be called by displayName alone; the fields marked ui:\"display\" are %q", marked)
	}
}
