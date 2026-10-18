package openai_test

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
)

// TestACompressedResponseIsBoundedByWhatItExpandsTo answers in gzip whenever the
// adapter's transport asks for it. A compressed body names no length the adapter can
// refuse up front, so the response bound has to hold over the bytes it expands to: a
// few kilobytes on the wire that inflate to megabytes are refused, and an ordinary
// answer in gzip still arrives whole.
func TestACompressedResponseIsBoundedByWhatItExpandsTo(t *testing.T) {
	inflated := `{"model":"vendor-model","choices":[{"message":{"role":"assistant","content":"` +
		strings.Repeat("a", 4<<20) + `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`
	for _, tc := range []struct {
		name string
		body string
		want error
	}{
		{"an ordinary answer arrives whole", answer, nil},
		{"an answer that inflates past the bound is refused", inflated, inference.ErrResponseTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
					io.WriteString(w, tc.body)
					return
				}
				w.Header().Set("Content-Encoding", "gzip")
				zw := gzip.NewWriter(w)
				io.WriteString(zw, tc.body)
				zw.Close()
			}))
			t.Cleanup(server.Close)
			got, err := dial(t, server).Chat(withTenant, ask)
			if tc.want == nil {
				if err != nil || got.Text != "a cat" {
					t.Fatalf("Chat(gzip answer) = %+v, %v; want the answer whole", got, err)
				}
				return
			}
			if !errors.Is(err, tc.want) || got.Text != "" {
				t.Fatalf("Chat(gzip answer of %d bytes) = %d bytes of text, %v; want no text and %v",
					len(tc.body), len(got.Text), err, tc.want)
			}
		})
	}
}
