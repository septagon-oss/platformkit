package entity_test

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"testing"
)

func TestExplicitDefaultsUseSchemaTypesAndClosedMapKeys(t *testing.T) {
	for _, tc := range []struct {
		kind  entity.FieldType
		elem  entity.FieldType
		value string
		valid bool
	}{
		{entity.TypeBool, "", "false", true}, {entity.TypeInt, "", "0", true},
		{entity.TypeUUID, "", "invalid", false}, {entity.TypeInt, "", "", false},
		{entity.TypeList, entity.TypeInt, "[9007199254740993]", true},
		{entity.TypeList, entity.TypeInt, "[1.5]", false}, {entity.TypeList, entity.TypeInt, "[null]", false},
		{entity.TypeList, entity.TypeString, "[1]", false}, {entity.TypeList, entity.TypeString, "[]", true},
		{entity.TypeList, entity.TypeString, "{}", false}, {entity.TypeMap, entity.TypeInt, "[]", false},
		{entity.TypeMap, entity.TypeInt, `{"yes":0}`, true}, {entity.TypeMap, entity.TypeInt, `{"no":0}`, false},
		{entity.TypeMap, entity.TypeInt, `{"yes":"0"}`, false}, {entity.TypeMap, entity.TypeInt, `{"yes":`, false},
	} {
		f := entity.Field{Name: "value", Type: tc.kind, Elem: tc.elem, Default: tc.value, Keys: []entity.MapKey{{Name: "yes"}}}
		_, err := entity.DefaultValue(f)
		if (err == nil) != tc.valid {
			t.Errorf("%s/%s %q: %v, valid=%v", tc.kind, tc.elem, tc.value, err, tc.valid)
		}
	}
	if _, err := entity.DefaultValue(entity.Field{Name: "state", Type: entity.TypeString, Default: "other", Enum: []string{"open"}}); err == nil {
		t.Fatal("off-enum default accepted")
	}
}
