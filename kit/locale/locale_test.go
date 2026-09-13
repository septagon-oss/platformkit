package locale_test

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
)

// A worker can supply readable source messages without an SDK or web renderer.
type sourceMessages struct{}

func (sourceMessages) Select(...string) locale.Locale {
	return locale.Locale{Language: "en", Formatter: sourceMessages{}}
}

func (sourceMessages) Text(_ string, fallback string, args ...any) string {
	return fmt.Sprintf(fallback, args...)
}

func ExampleSelectLocale() {
	messages := locale.SelectLocale(sourceMessages{}, "en")
	fmt.Println(messages.Language)
	fmt.Println(messages.Text("task.digest", "%d tasks need your review", 2))
	// Output:
	// en
	// 2 tasks need your review
}

type incompleteMessages struct{ locale.Locale }

func (m incompleteMessages) Select(...string) locale.Locale { return m.Locale }

func TestIncompleteConfigurationCannotReachAWorkerRender(t *testing.T) {
	for _, tc := range []struct {
		name     string
		messages locale.Messages
	}{
		{"missing provider", nil},
		{"missing language", incompleteMessages{locale.Locale{Formatter: sourceMessages{}}}},
		{"missing formatter", incompleteMessages{locale.Locale{Language: "en"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("incomplete message configuration reached rendering")
				}
			}()
			_ = locale.SelectLocale(tc.messages)
		})
	}
}
