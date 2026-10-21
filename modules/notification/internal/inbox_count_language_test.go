package internal

import (
	"fmt"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

func TestTheInboxCountUsesTheReadersPlural(t *testing.T) {
	catalog := xtext.Load("en", xtext.Source{Name: "notification", FS: os.DirFS("../messages")})
	for _, tag := range []string{"en", "pt-PT"} {
		for _, count := range []int64{0, 1, 2, 5} {
			t.Run(fmt.Sprintf("%s/%d", tag, count), func(t *testing.T) {
				selected := catalog.Select(tag)
				phrase := "%d notices, newest first. Only the person a notice is addressed to sees it here."
				if tag == "pt-PT" {
					phrase = "%d notificações, mais recente primeiro. Só a pessoa a quem são endereçadas as vê aqui."
				}
				if count == 1 && tag == "en" {
					phrase = "%d notice, newest first. Only the person a notice is addressed to sees it here."
				} else if count == 1 {
					phrase = "%d notificação, mais recente primeiro. Só a pessoa a quem é endereçada a vê aqui."
				}
				if got, want := inboxCount(&selected, count), fmt.Sprintf(phrase, count); got != want {
					t.Errorf("inbox count = %q, want %q", got, want)
				}
			})
		}
	}
}
