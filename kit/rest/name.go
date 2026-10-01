package rest

import (
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// Name is the singular and plural English source name declared by a resource.
type Name = entity.Name

func nameFault(name Name) string {
	for _, field := range []struct{ label, value string }{{"One", name.One}, {"Other", name.Other}} {
		if strings.TrimSpace(field.value) == "" || strings.TrimSpace(field.value) != field.value || strings.Contains(field.value, "%") {
			return "Name." + field.label + " is required without surrounding whitespace or format verbs"
		}
	}
	return ""
}
