package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The six questions decision 0011 asks every module README to answer, as the
// subsection headings of its `## Authorization` section, in this order.
var authorizationQuestions = []string{
	"### Permissions",
	"### Object scope",
	"### Duties the module enforces itself",
	"### Public faces",
	"### The operator boundary",
	"### Provisioning",
}

// TestEveryComposedModuleREADMEAnswersTheSixAuthorizationQuestions is T-0116's gate:
// a module README without its Authorization section is refused, and the section is
// read against the manifest rather than trusted — every permission key the module
// declares must be named in its Permissions subsection, and every operator-only key
// in its operator-boundary subsection. So the documentation of who may do what
// cannot drift from the declaration the kernel enforces.
// declaredElsewhere is the census's one exception, written down rather than
// guessed: three of the modules the reference composition names are not a
// directory of their own, so the README that answers for them is not the one the
// rule `modules/<name>/README.md` would find. `product` and `access` are declared
// by this application in product.go, and their answers belong in this
// application's own README; `emailregistration` is the registration policy the
// auth package declares beside its module, so the auth README answers for it.
// Every other name the composition resolves is a directory, and a kernel module
// that grows a README-less sibling still fails here.
var declaredElsewhere = map[string]string{
	"product":           "README.md",
	"access":            "README.md",
	"emailregistration": filepath.Join("..", "..", "modules", "auth", "README.md"),
}

func TestEveryComposedModuleREADMEAnswersTheSixAuthorizationQuestions(t *testing.T) {
	_, cfg := configure(t)
	c := compose(cfg)
	if len(c.modules) == 0 {
		t.Fatal("the reference application composes no module, so this case would prove nothing")
	}
	for _, m := range c.modules {
		t.Run(m.Name, func(t *testing.T) {
			path, listed := declaredElsewhere[m.Name]
			if !listed {
				path = filepath.Join("..", "..", "modules", m.Name, "README.md")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v — every composed module has a README", path, err)
			}
			text := string(raw)
			start := strings.Index(text, "\n## Authorization\n")
			if start < 0 {
				t.Fatalf("%s has no `## Authorization` section (decision 0011)", path)
			}
			section := text[start+1:]
			if next := strings.Index(section[len("## Authorization\n"):], "\n## "); next >= 0 {
				section = section[:len("## Authorization\n")+next]
			}
			sub := map[string]string{}
			at := 0
			for i, q := range authorizationQuestions {
				idx := strings.Index(section[at:], "\n"+q+"\n")
				if idx < 0 {
					t.Fatalf("%s's Authorization section lacks %q, question %d of decision 0011, in order", path, q, i+1)
				}
				body := section[at+idx+len(q)+2:]
				if next := strings.Index(body, "\n### "); next >= 0 {
					body = body[:next]
				}
				if strings.TrimSpace(body) == "" {
					t.Errorf("%s answers %q with nothing", path, q)
				}
				sub[q] = body
				at += idx + len(q) + 1
			}
			for _, p := range m.Permissions {
				if !strings.Contains(sub["### Permissions"], "`"+p.Key+"`") {
					t.Errorf("%s's Permissions subsection does not name `%s`, which the manifest declares", path, p.Key)
				}
				if p.Operator && !strings.Contains(sub["### The operator boundary"], "`"+p.Key+"`") {
					t.Errorf("%s's operator-boundary subsection does not name `%s`, which the manifest marks the operator's", path, p.Key)
				}
			}
		})
	}
}
