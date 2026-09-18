package components_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/components"
)

// The derivation is tested through the rendered disc rather than a test-only
// export: what a product depends on is the markup, and a helper made public for
// tests is a permanent API bought for a temporary convenience.
func TestAvatarInitialsAreTheFirstWordAndTheLast(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"Jean-Paul Reyes", "JR"},
		{"ada", "A"}, // one word is one letter; the rest of it is not a puzzle to solve
		{"  Ada   Lovelace  ", "AL"},
		{"Grace Brewster Murray Hopper", "GH"}, // first word and last word, not all of them
		{"ÿnn", "Ÿ"},                           // runes, not bytes: a name is not ASCII-only
		{"", ""},
		{"   ", ""},
	} {
		out := html(t, components.Avatar(components.AvatarProps{Name: tc.name}))
		if tc.want == "" {
			if strings.Contains(out, "</span>") {
				t.Errorf("a name with no letters must yield no letters, not a guess: %s", out)
			}
			continue
		}
		if !strings.Contains(out, ">"+tc.want+"</span>") {
			t.Errorf("initials(%q): want %q in %s", tc.name, tc.want, out)
		}
	}
}

func TestAvatarIsNamedAfterThePersonAndNotItsContents(t *testing.T) {
	out := html(t, components.Avatar(components.AvatarProps{Name: "Jean-Paul Reyes"}))
	if !strings.Contains(out, `aria-label="Jean-Paul Reyes"`) {
		t.Errorf("the disc must carry the person's name; got %s", out)
	}
	if !strings.Contains(out, `role="img"`) {
		t.Error("an avatar that speaks is an image to a screen reader, so it must say so")
	}
	// The letters themselves are decoration: read as "J P R" they are noise on
	// every row of a list of people.
	i := strings.Index(out, ">JR<")
	if i < 0 {
		t.Fatalf("no initials rendered: %s", out)
	}
	if before := out[:i]; !strings.Contains(before, `aria-hidden="true"`) {
		t.Errorf("initials must be aria-hidden or a reader spells them out; got %s", out)
	}
}

func TestAvatarBesideItsOwnNameSaysNothing(t *testing.T) {
	out := html(t, components.Avatar(components.AvatarProps{
		Name: "Ada Lovelace", Decorative: true,
	}))
	if strings.Contains(out, "aria-label") {
		t.Errorf("a decorative disc repeats a name already on screen: %s", out)
	}
	if !strings.Contains(out, `aria-hidden="true"`) {
		t.Errorf("Decorative must hide the whole disc from a reader: %s", out)
	}
}

func TestAvatarPictureGoesThroughMediaNotASecondImg(t *testing.T) {
	out := html(t, components.Avatar(components.AvatarProps{
		Name: "Ada Lovelace", Src: "/artwork/ada.png",
	}))
	if !strings.Contains(out, `src="/artwork/ada.png"`) || !strings.Contains(out, `alt="Ada Lovelace"`) {
		t.Fatalf("picture lost its name: %s", out)
	}
	// Both of these are Media's decisions, inherited and not re-implemented: a
	// disc that rolled its own <img> would be the second picture renderer in this
	// package, which is the drift Avatar exists to avoid.
	if !strings.Contains(out, `loading="lazy"`) {
		t.Errorf("a picture below the fold of a member list must not block the page: %s", out)
	}
	if !strings.Contains(out, "object-cover") {
		t.Errorf("an image in a fixed round box has to be cropped or it hangs over: %s", out)
	}
	if n := strings.Count(out, "<img"); n != 1 {
		t.Errorf("one disc, one picture; got %d in %s", n, out)
	}
}

func TestAvatarWithNeitherNameNorPictureInventsNothing(t *testing.T) {
	out := html(t, components.Avatar(components.AvatarProps{}))
	if !strings.Contains(out, "<svg") {
		t.Errorf("with nothing to show, the glyph stands in: %s", out)
	}
	for _, banned := range []string{"aria-label", ">?<", ">NULL<", ">Unknown<"} {
		if strings.Contains(out, banned) {
			t.Errorf("renderer invented %q behind an empty disc: %s", banned, out)
		}
	}
}

func TestAvatarLinkIsTheDiscAndNamesItselfOnce(t *testing.T) {
	out := html(t, components.Avatar(components.AvatarProps{Name: "Ada Lovelace", Href: "/people/ada"}))
	if !strings.HasPrefix(out, `<a `) || !strings.Contains(out, `href="/people/ada"`) {
		t.Fatalf("a linked disc is the anchor itself, not a div with one inside: %s", out)
	}
	if strings.Count(out, "aria-label") != 1 {
		t.Errorf("a link and its contents must not both speak: %s", out)
	}
	// Measure the anchor's own start tag, not the page: aria-hidden anywhere would
	// match, and the bug is specifically which element carries it.
	tag := out[:strings.Index(out, ">")]
	if strings.Contains(tag, `aria-hidden`) {
		t.Errorf("the anchor itself is hidden, so a keyboard user reaches a person and hears nothing: %s", out)
	}
	if !strings.Contains(out, `aria-label="Ada Lovelace"`) {
		t.Errorf("the link must name the person: %s", out)
	}
}

func TestAvatarUnknownSizeFallsBackToTheShelfDefault(t *testing.T) {
	want := html(t, components.Avatar(components.AvatarProps{Name: "Ada"}))
	got := html(t, components.Avatar(components.AvatarProps{Name: "Ada", Size: "colossal"}))
	if got != want {
		t.Errorf("an unknown size must render md, the way every variant here does:\n %s\n %s", got, want)
	}
}

func TestAvatarGolden(t *testing.T) {
	const want = `<div class="flex items-center justify-center overflow-hidden flex-shrink-0 bg-surface-secondary text-fg-secondary font-semibold w-12 h-12 rounded-full" role="img" aria-label="Jean-Paul Reyes"><span class="text-sm" aria-hidden="true">JR</span></div>`
	if got := html(t, components.Avatar(components.AvatarProps{Name: "Jean-Paul Reyes"})); got != want {
		t.Errorf("avatar moved:\n got %s\nwant %s", got, want)
	}
}

func TestMediaFitIsHonoured(t *testing.T) {
	cover := html(t, components.Media(components.MediaProps{Src: "/a.png", Alt: "A", Fit: "cover"}))
	if !strings.Contains(cover, "object-cover") {
		t.Errorf("cover promised a crop and delivered none: %s", cover)
	}
	plain := html(t, components.Media(components.MediaProps{Src: "/a.png", Alt: "A"}))
	if !strings.Contains(plain, "object-cover") {
		t.Errorf("the default crop is cover, which is what Card has always done: %s", plain)
	}
	if strings.Contains(plain, "object-contain") {
		t.Errorf("two crops on one element is a fight decided by the stylesheet, not the caller: %s", plain)
	}
	// The case the first cut lost: asking for contain must not leave a crop class
	// behind from the card geometry it inherited.
	each := html(t, components.Media(components.MediaProps{Src: "/a.png", Alt: "A", Fit: "contain"}))
	if strings.Contains(each, "object-cover") || !strings.Contains(each, "object-contain") {
		t.Errorf("contain asked for and not delivered: %s", each)
	}
}
