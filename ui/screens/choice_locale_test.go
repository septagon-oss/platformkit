package screens_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/screens"
)

func TestChoiceLocalesDetachCommandAndResourceWords(t *testing.T) {
	r := resource()
	f := choiceCatalogField("figure")
	f.Choices.Maximum = &entity.ChoiceMaximum{Count: 2, Behaviour: "refuse", Message: "At most two."}
	r.Schema.Fields = []entity.Field{f}
	r.Commands = []httpx.Command{{Verb: "choose", Fields: []entity.Field{f}}}
	var workers sync.WaitGroup
	for _, language := range []string{"en", "pt-PT"} {
		workers.Go(func() {
			text := func(_ string, fallback string) string { return language + ":" + fallback }
			local := screens.Localise(r, text)
			entry := screens.DescribeEntry(local, true, text, language)
			for _, field := range []entity.Field{local.Schema.Fields[0], local.Commands[0].Fields[0]} {
				if !strings.HasPrefix(field.Choices.Options[0].Label, language+":") || field.Choices.Options[0].ID != "round" || field.Presentation.Figure.SVG() != f.Presentation.Figure.SVG() {
					t.Errorf("locale changed identity or lost words: %+v", field)
				}
				field.Choices.Options[1].Group.Label = "Changed"
				field.Choices.Maximum.Count = 1
			}
			resolved := entry.Resolved.Fields[0]
			resolved.Options[0].Label = "Changed"
			resolved.Choice.Maximum.Count = 1
			resolved.Choice.Groups[0].Label = "Changed"
		})
	}
	workers.Wait()
	if f.Choices.Options[0].Label != "Round" || f.Choices.Options[1].Group.Label != "Corners" || f.Choices.Maximum.Count != 2 || r.Commands[0].Fields[0].Choices.Empty.Message != "Keep one shape." {
		t.Fatal("one locale mutated the shared declaration")
	}
}
