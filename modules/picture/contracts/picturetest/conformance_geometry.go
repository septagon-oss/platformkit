package picturetest

import (
	"strings"
	"testing"
)

func invalidGeometry(t *testing.T, factory func() Decision) {
	for _, v := range []string{"NaN", "Inf", "1e309", "1000001", "1%", "1px", "1,", "1-2", "-1"} {
		t.Run(v, func(t *testing.T) { badTemplate(t, factory(), strings.Replace(SVG, `r="12"`, `r="`+v+`"`, 1), "r") })
	}
}
