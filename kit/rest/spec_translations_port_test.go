package rest_test

import (
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/rest"
)

// TestASpecIsHandedItsTranslationsPort: a Spec that declares a translatable
// field is wired to the translation module by one field set at composition —
// `contentSpec.Translations = translationSvc`, as the module README writes it.
func TestASpecIsHandedItsTranslationsPort(t *testing.T) {
	field, ok := reflect.TypeOf(rest.Spec[*Plan]{}).FieldByName("Translations")
	if !ok {
		t.Fatal("rest.Spec has no Translations field to wire the port into")
	}
	if want := reflect.TypeOf((*rest.Translations)(nil)).Elem(); field.Type != want {
		t.Errorf("rest.Spec.Translations is %v, want %v", field.Type, want)
	}
}
