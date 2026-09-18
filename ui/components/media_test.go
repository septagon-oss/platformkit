package components_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components"
	g "maragu.dev/gomponents"
)

// A picture is the one part of a page that fails invisibly: an <img> is emitted
// whether or not there is anything at the other end, and every state that is not
// "it arrived" paints the same broken glyph. These cases pin that only one of the
// five emits an <img>, and that the other four say which of them this is.

func renderMedia(t *testing.T, node g.Node) string {
	t.Helper()
	builder := &strings.Builder{}
	if err := node.Render(builder); err != nil {
		t.Fatalf("render: %v", err)
	}
	return builder.String()
}

func TestMediaOnlyRendersAPictureWhenItHasOne(t *testing.T) {
	t.Parallel()
	states := []struct {
		name   string
		status components.MediaStatus
	}{
		{"waiting", components.MediaLoading},
		{"nothing here", components.MediaEmpty},
		{"could not get it", components.MediaFailed},
		{"may not see it", components.MediaRefused},
		{"a status from a newer vocabulary", components.MediaStatus("queued")},
	}
	for _, state := range states {
		html := renderMedia(t, components.Media(components.MediaProps{
			Status: state.status, Src: "/api/v1/trace/cuts/1/image/cutout", Alt: "The cut-out",
			Reason: "the engine is not answering",
		}))
		if strings.Contains(html, "<img") {
			t.Errorf("%s renders a picture it does not have:\n%s", state.name, html)
		}
		if !strings.Contains(html, "the engine is not answering") && state.name != "waiting" {
			t.Errorf("%s hides the caller's reason:\n%s", state.name, html)
		}
	}
	html := renderMedia(t, components.Media(components.MediaProps{Src: "/a.png", Alt: "A"}))
	if !strings.Contains(html, `<img src="/a.png"`) {
		t.Errorf("ready renders no picture:\n%s", html)
	}
}

func TestMediaRefusalIsNotRenderedAsEmptiness(t *testing.T) {
	t.Parallel()
	refused := renderMedia(t, components.Media(components.MediaProps{Status: components.MediaRefused, Reason: "not in your tier"}))
	empty := renderMedia(t, components.Media(components.MediaProps{Status: components.MediaEmpty, Reason: "not in your tier"}))
	if refused != empty {
		return // distinct markup is the requirement; either form is fine so long as
	}
	t.Errorf("a refusal renders identically to an empty shelf, so the page says \"nothing here\" about a permission:\n%s", refused)
}

func TestMediaLoadingHoldsTheBoxAndSaysSo(t *testing.T) {
	t.Parallel()
	html := renderMedia(t, components.Media(components.MediaProps{Status: components.MediaLoading, Alt: "Cutting the subject out"}))
	for _, want := range []string{`aria-busy="true"`, `role="img"`, `aria-label="Cutting the subject out"`} {
		if !strings.Contains(html, want) {
			t.Errorf("waiting media lacks %s:\n%s", want, html)
		}
	}
}

// The intrinsic size is what keeps the page still: without width and height
// attributes the box is nought tall until the bytes land, and everything below it
// jumps when they do.
func TestMediaReservesItsOwnSpace(t *testing.T) {
	t.Parallel()
	html := renderMedia(t, components.Media(components.MediaProps{Src: "/a.png", Alt: "A", Width: 920, Height: 540, Lazy: true}))
	for _, want := range []string{`width="920"`, `height="540"`, `loading="lazy"`} {
		if !strings.Contains(html, want) {
			t.Errorf("media does not carry %s:\n%s", want, html)
		}
	}
	if strings.Contains(html, `width="920" height="0"`) {
		t.Errorf("half a size is not a size:\n%s", html)
	}
}

// Card already emitted exactly this element. Delegating to Media must not move a
// byte of it, because the export digest is byte-sensitive and a card's picture is
// rendered on pages this repository does not own.
func TestMediaDoesNotMoveWhatCardAlreadyRendered(t *testing.T) {
	t.Parallel()
	html := renderMedia(t, components.Media(components.MediaProps{Src: "/art.png", Alt: "Sheet art"}))
	if want := `<img src="/art.png" alt="Sheet art" class="`; !strings.Contains(html, want) {
		t.Errorf("a plain picture is no longer the element Card used to write.\ngot:  %s\nwant: %s…", html, want)
	}
	if strings.Contains(html, "width=") || strings.Contains(html, "figure") {
		t.Errorf("a picture with no size and no caption grew markup nobody asked for:\n%s", html)
	}
	// The thumbnail Card renders beside text, which is the other byte this test
	// exists for: delegation that quietly changes it would move a digest and
	// every page that reads it.
	thumbnail := renderMedia(t, components.Media(components.MediaProps{Src: "/art.png", Alt: "Sheet art", Horizontal: true}))
	if !strings.Contains(thumbnail, `<img src="/art.png" alt="Sheet art" class="w-48`) {
		t.Errorf("the horizontal form is no longer Card's thumbnail:\n%s", thumbnail)
	}
	if strings.Contains(thumbnail, "w-full") {
		t.Errorf("a thumbnail asked for the full-width class as well:\n%s", thumbnail)
	}
}

func TestMediaCaptionIsAcaptionNotAltText(t *testing.T) {
	t.Parallel()
	html := renderMedia(t, components.Media(components.MediaProps{Src: "/a.png", Alt: "A", Caption: "Proof plate, 592×400 mm"}))
	for _, want := range []string{"<figcaption", "<!--pk-text:caption-->", "Proof plate, 592×400 mm"} {
		if !strings.Contains(html, want) {
			t.Errorf("captioned media lacks %s:\n%s", want, html)
		}
	}
	if strings.Contains(html, `alt="Proof plate`) {
		t.Errorf("the caption was folded into alt, so it is read once and cannot be returned to:\n%s", html)
	}
}
