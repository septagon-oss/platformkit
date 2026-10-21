package httpx

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// BindingFault validates argument and row names, scalar types and ownership.
// Bindings affect presentation only; submitted values still require validation.
func BindingFault(arguments, row []entity.Field, bindings map[string]string) string {
	for _, name := range slices.Sorted(maps.Keys(bindings)) {
		arg, a := entity.FieldNamed(arguments, name)
		field, b := entity.FieldNamed(row, bindings[name])
		reserved := strings.ToLower(strings.ReplaceAll(name, "_", ""))
		if !a || !b || entity.ServerOwned(arg) || slices.Contains([]string{"person", "personid", "actor", "actorid", "tenant", "tenantid"}, reserved) {
			return fmt.Sprintf("binding %q names no caller-owned argument or row field", name)
		}
		if arg.Type != field.Type || !slices.Contains([]entity.FieldType{entity.TypeUUID, entity.TypeString, entity.TypeText, entity.TypeInt, entity.TypeFloat, entity.TypeBool, entity.TypeTime}, arg.Type) {
			return fmt.Sprintf("binding %q requires compatible scalar fields", name)
		}
	}
	return ""
}
