package components

import (
	"strings"
	"testing"
)

func TestVideoRetainsNativeControlsAndCaptionMetadata(t *testing.T) {
	p := VideoProps{Label: "Trace a value", Poster: "/poster.png",
		Sources: []VideoSource{{Src: "/lesson.mp4", Type: "video/mp4"}, {Src: "/lesson.webm", Type: "video/webm"}},
		Tracks:  []VideoTrack{{Src: "/en.vtt", Language: "en", Label: "English captions", Default: true}, {Src: "/pt.vtt", Language: "pt", Label: "Português", Kind: "subtitles"}},
	}
	out := renderNode(t, Video(p))
	for _, want := range []string{`<video`, `controls`, `playsinline`, `preload="metadata"`, `aria-label="Trace a value"`, `poster="/poster.png"`, `type="video/mp4"`, `src="/lesson.webm"`, `kind="captions"`, `srclang="en"`, `label="English captions"`, `kind="subtitles"`, `srclang="pt"`, `default`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	for _, unwanted := range []string{"autoplay", "<script", "<iframe", "tabindex=\"1\""} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %s", unwanted)
		}
	}
	if strings.Index(out, "<track") < strings.LastIndex(out, "<source") {
		t.Fatal("sources must precede text tracks")
	}
}

func TestDisabledVideoCannotLoadOrPlayMedia(t *testing.T) {
	out := renderNode(t, Video(VideoProps{ComponentProps: ComponentProps{Disabled: true}, Label: "Unavailable", Sources: []VideoSource{{Src: "/secret.mp4", Type: "video/mp4"}}, Tracks: []VideoTrack{{Src: "/secret.vtt"}}}))
	if strings.Contains(out, "controls") || strings.Contains(out, "secret") || !strings.Contains(out, `aria-disabled="true"`) {
		t.Fatalf("disabled video remains operable: %s", out)
	}
}

func TestVideoEscapesLabelsAndSelectsOnlyOneDefaultTrack(t *testing.T) {
	out := renderNode(t, Video(VideoProps{Label: `" onplay="bad()`, Tracks: []VideoTrack{{Src: "/en.vtt", Label: "<script>", Default: true}, {Src: "/pt.vtt", Default: true}}}))
	if strings.Contains(out, `<script>`) || strings.Contains(out, ` onplay="`) || strings.Count(out, " default") != 1 {
		t.Fatalf("unsafe label or ambiguous defaults: %s", out)
	}
}
