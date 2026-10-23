package picturetest

import (
	"strings"
	"testing"
)

func invalidTemplates(t *testing.T, factory func() Decision) {
	cases := []struct{ name, source, detail string }{
		{"no namespace", strings.Replace(SVG, ` xmlns="http://www.w3.org/2000/svg"`, "", 1), "namespace"},
		{"wrong namespace", strings.Replace(SVG, "http://www.w3.org/2000/svg", "urn:other", 1), "namespace"},
		{"child namespace", strings.Replace(SVG, "<circle", `<circle xmlns=""`, 1), "namespace"},
		{"namespace prefix", strings.Replace(SVG, "<circle", `<s:circle`, 1), "circle"},
		{"duplicate attribute", strings.Replace(SVG, "cx=", `fill="#000000" cx=`, 1), "duplicate"},
		{"DTD", `<!DOCTYPE svg>` + SVG, "directive"},
		{"external entity", `<!DOCTYPE svg [<!ENTITY x SYSTEM "file:///etc/passwd">]>` + SVG, "directive"},
		{"recursive entity", `<!DOCTYPE svg [<!ENTITY x "&x;">]>` + SVG, "directive"},
		{"unknown entity", strings.Replace(SVG, "#010203", "&unknown;", 1), "XML"},
		{"stylesheet", `<?xml-stylesheet href="x"?>` + SVG, "processing instruction"},
		{"non-UTF-8 encoding", `<?xml version="1.0" encoding="ISO-8859-1"?>` + SVG, "XML processing instruction"},
		{"XML 1.1", `<?xml version="1.1"?>` + SVG, "processing instruction"},
		{"standalone no", `<?xml version="1.0" standalone="no"?>` + SVG, "processing instruction"},
		{"BOM", "\ufeff" + SVG, "UTF-8"},
		{"control in comment", strings.Replace(SVG, "</svg>", "<!--\x01--></svg>", 1), "XML"},
		{"noncharacter in comment", strings.Replace(SVG, "</svg>", "<!--\uffff--></svg>", 1), "XML"}, {"invalid UTF8", "\xff" + SVG, "UTF-8"},
		{"text", strings.Replace(SVG, "</svg>", "hello</svg>", 1), "character data"},
		{"CDATA", strings.Replace(SVG, "</svg>", "<![CDATA[hello]]></svg>", 1), "character data"},
		{"unclosed", strings.TrimSuffix(SVG, "</svg>"), "complete"},
		{"two roots", SVG + SVG, "root"},
		{"too large", strings.Repeat(" ", 262145), "262144"},
		{"too deep", `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1">` + strings.Repeat("<g>", 32) + strings.Repeat("</g>", 32) + "</svg>", "depth"},
		{"too many elements", strings.Replace(SVG, "</svg>", strings.Repeat("<g/>", 2048)+"</svg>", 1), "2048"},
		{"long attribute", strings.Replace(SVG, "<circle", `<circle transform="`+strings.Repeat(" ", 65537)+`"`, 1), "65536"},
		{"missing viewBox", strings.Replace(SVG, ` viewBox="0 0 100 80"`, "", 1), "viewBox"},
		{"paired markers", strings.Replace(SVG, ` data-option="round"`, "", 1), "together"},
		{"invalid part", strings.Replace(SVG, `data-part="shape"`, `data-part="Shape"`, 1), "data-part"},
		{"invalid slot", strings.Replace(SVG, `data-slot="primary"`, `data-slot="fill:primary"`, 1), "data-slot"},
		{"nested options", strings.Replace(SVG, "<circle", `<g data-part="outer" data-option="one"><circle`, 1), "nested"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { badTemplate(t, factory(), c.source, c.detail) })
	}
}
