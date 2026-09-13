package components_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components"
)

func TestCheckboxAssociatesItsOwnErrorAndHelp(t *testing.T) {
	t.Parallel()
	for _, help := range []string{"", "Read the terms."} {
		props := components.CheckboxProps{ComponentProps: components.ComponentProps{ID: "terms"}, Name: "terms", Label: "I accept",
			Value: "true", Checked: true, Required: true, HelpText: help, Error: "Confirm <these> terms"}
		var out strings.Builder
		if err := components.Checkbox(props).Render(&out); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		describedBy := "terms-error"
		if help != "" {
			describedBy += " terms-help"
		}
		for _, want := range []string{`for="terms"`, `id="terms"`, `type="checkbox"`, `checked`, `required`,
			`aria-invalid="true"`, `aria-describedby="` + describedBy + `"`, `id="terms-error"`, `Confirm &lt;these&gt; terms`} {
			if !strings.Contains(body, want) {
				t.Fatalf("checkbox lacks %s: %s", want, body)
			}
		}
		props.Error = ""
		out.Reset()
		if err := components.Checkbox(props).Render(&out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "terms-error") || strings.Contains(out.String(), "aria-invalid") {
			t.Fatal("a valid checkbox retained an error relationship")
		}
	}
}
