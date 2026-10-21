package internal

import (
	"fmt"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

func TestTheQueueCountUsesTheReadersPlural(t *testing.T) {
	catalog := xtext.Load("en", xtext.Source{Name: "change", FS: os.DirFS("../messages")})
	for _, tag := range []string{"en", "pt-PT"} {
		for _, count := range []int64{0, 1, 2, 5} {
			t.Run(fmt.Sprintf("%s/%d", tag, count), func(t *testing.T) {
				selected := catalog.Select(tag)
				phrase := "%d proposals, newest first. Each one waits for an account other than the person who wrote it."
				if tag == "pt-PT" {
					phrase = "%d propostas, mais recente primeiro. Cada proposta espera a decisão de outra conta, não de quem as escreveu."
				}
				if count == 1 && tag == "en" {
					phrase = "%d proposal, newest first. Each one waits for an account other than the person who wrote it."
				} else if count == 1 {
					phrase = "%d proposta, mais recente primeiro. Cada proposta espera a decisão de outra conta, não de quem a escreveu."
				}
				if got, want := queueCount(&selected, count), fmt.Sprintf(phrase, count); got != want {
					t.Errorf("queue count = %q, want %q", got, want)
				}
			})
		}
	}
}
