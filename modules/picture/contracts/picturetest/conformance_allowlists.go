package picturetest

import (
	"strings"
	"testing"
)

func closedElements(t *testing.T, factory func() Decision) {
	for _, element := range strings.Fields("script foreignObject style image use a animate animateTransform set filter feImage text tspan font linearGradient radialGradient pattern defs clipPath mask svg unknown") {
		t.Run(element, func(t *testing.T) {
			source := strings.Replace(SVG, "</svg>", "<"+element+"/></svg>", 1)
			badTemplate(t, factory(), source, element)
		})
	}
}

func closedAttributes(t *testing.T, factory func() Decision) {
	for _, attr := range strings.Fields("onload onclick ONLOAD style class id data-other href src stroke-dasharray filter clip-path xlink:href") {
		t.Run(attr, func(t *testing.T) { badTemplate(t, factory(), strings.Replace(SVG, "cx=", attr+`="x" cx=`, 1), attr) })
	}
}

func unsafePaint(t *testing.T, factory func() Decision) {
	for _, property := range []string{"fill", "stroke"} {
		for _, value := range []string{"red", "url(https://invalid.example/x)", "url(#local)", "data:image/svg+xml,x", "var(--x)", "currentColor", "inherit", "#abc", "#11223344"} {
			t.Run(property+value, func(t *testing.T) {
				badTemplate(t, factory(), strings.Replace(SVG, property+`="#010203"`, property+`="`+value+`"`, 1), property)
			})
		}
	}
}
