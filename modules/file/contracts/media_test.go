package contracts_test

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func TestInlineMediaRequiresItsDeclaredSignature(t *testing.T) {
	for _, tc := range []struct {
		name, kind, bytes string
		valid             bool
	}{
		{"mp4", "video/mp4", "\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isom", true},
		{"webm", "video/webm", "\x1a\x45\xdf\xa3", true},
		{"captions", "text/vtt; charset=utf-8", "WEBVTT\n\n00:00.000 --> 00:01.000\nHello\n", true},
		{"caption BOM", "text/vtt", "\xef\xbb\xbfWEBVTT\r\n", true},
		{"caption title", "text/vtt", "WEBVTT English\n", true},
		{"html as video", "video/mp4", "<html><script>alert(1)</script></html>", false},
		{"opaque video", "video/webm", "\x00\x00\x00\x00", false},
		{"plain text captions", "text/vtt", "not a WebVTT file", false},
		{"false header", "text/vtt", "WEBVTTbad\n", false},
		{"caption arrow in header", "text/vtt", "WEBVTT -->\n", false},
		{"html as captions", "text/vtt", "<html>WEBVTT</html>", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !contracts.Renderable(tc.kind) {
				t.Fatal("media is downloaded as an attachment")
			}
			err := contracts.Agrees(tc.kind, []byte(tc.bytes))
			if tc.valid && err != nil || !tc.valid && !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("valid=%v: %v", tc.valid, err)
			}
		})
	}
}
