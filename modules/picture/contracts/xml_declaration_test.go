package contracts_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/picture/contracts"
	"github.com/septagon-oss/platformkit/modules/picture/contracts/picturetest"
)

// Only one initial XML 1.0 UTF-8 declaration may precede a template, and the
// refusal names the processing instruction rather than a generic parse failure;
// an accepted declaration composes exactly the undeclared picture.
func TestOnlyAnInitialUTF8DeclarationPrecedesATemplate(t *testing.T) {
	for _, prefix := range []string{
		`<?xml version="1.0" encoding="UTF-16"?>`,
		`<?xml version="1.0" encoding="utf8"?>`,
		`<?xml version="1.0" encoding="ISO-8859-1"?>`,
		`<?xml version="1.0" encoding="UTF-8"`,
		`<?xml version="1.0"?><?xml version="1.0"?>`,
		` <?xml version="1.0"?>`,
		`<?XML version="1.0"?>`,
		`<!-- first --><?xml version="1.0"?>`,
		`<?xml-stylesheet href="https://invalid.example/x.css"?>`,
	} {
		t.Run(prefix, func(t *testing.T) {
			template, err := contracts.Compile("fixture", prefix+picturetest.SVG)
			if err == nil || !strings.Contains(err.Error(), "processing instruction") {
				t.Fatalf("refusal = %v, want one naming the processing instruction", err)
			}
			if strings.Contains(err.Error(), "<svg") {
				t.Fatalf("refusal %q quotes the source", err)
			}
			if _, err := contracts.Resolve(template, picturetest.Value()); err == nil {
				t.Fatal("a refused declaration resolved a value")
			}
		})
	}
	for _, prefix := range []string{
		`<?xml version = "1.0"  encoding = "UTF-8" ?>`,
		"<?xml\tversion='1.0'\n?>",
	} {
		t.Run(prefix, func(t *testing.T) {
			template, err := contracts.Compile("fixture", prefix+picturetest.SVG)
			if err != nil {
				t.Fatal(err)
			}
			resolved, err := contracts.Resolve(template, picturetest.Value())
			if err != nil {
				t.Fatal(err)
			}
			if body, err := contracts.Compose(template, resolved); err != nil || body != picturetest.Oracle {
				t.Fatalf("declared output %q: %v", body, err)
			}
		})
	}
}
