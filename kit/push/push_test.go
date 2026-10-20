package push_test

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/push"
	"github.com/septagon-oss/platformkit/kit/push/providers/log"
	"github.com/septagon-oss/platformkit/kit/push/pushtest"
)

// Adapters.Send is the router, and the two cases below are the ones a caller
// cannot see from the outside: a deployment that wired a platform to nothing must
// not be an installation that carries it, and a send that reaches nobody has to
// say which platform it could not reach rather than fail silently.
func TestARouterRefusesWhatItCarriesNoAdapterFor(t *testing.T) {
	hub := log.NewHub()
	carried := hub.Adapters()

	empty := push.Adapters{}
	if _, err := empty.Send(t.Context(), device(push.PlatformExpo), message()); err == nil {
		t.Error("a router with nothing behind it carried a message")
	} else if !isGone(err) && !strings.Contains(err.Error(), string(push.PlatformExpo)) {
		t.Errorf("the refusal says %q, which does not name the platform it could not carry", err)
	}

	nilKey := push.Adapters{push.PlatformExpo: nil}
	if _, err := nilKey.Send(t.Context(), device(push.PlatformExpo), message()); err == nil {
		t.Error("a platform wired to nil carried a message")
	}
	if active := nilKey.Active(); len(active) != 0 {
		t.Errorf("a key wired to nil is listed as carried: %v", active)
	}
	if got := carried.Get(push.PlatformWeb); got == nil {
		t.Fatal("the log adapter carries no web device, so every case below asserts nothing")
	}
	if active := carried.Active(); len(active) != len(push.Platforms) {
		t.Errorf("the log adapter carries %v, want every platform %v", active, push.Platforms)
	}

	receipt, err := carried.Send(t.Context(), device(push.PlatformExpo), message())
	if err != nil {
		t.Fatalf("a platform with an adapter behind it: %v", err)
	}
	if receipt.State != push.Delivered {
		t.Errorf("the send answers %q, want the state the adapter's own platform gives", receipt.State)
	}
	if len(hub.Sent()) != 1 {
		t.Errorf("the routed send left %d records behind, want one", len(hub.Sent()))
	}
}

// The clip is the one place a too-long notice is decided, and it is decided
// downwards rather than refused: the person is told shortly, or not at all.
func TestALongNoticeIsCarriedShorterRatherThanNotAtAll(t *testing.T) {
	long := message()
	long.Body = strings.Repeat("héllo", push.MaxBody)
	clipped := long.Clip()
	if len(clipped.Body) > push.MaxBody {
		t.Errorf("the clip left %d bytes, more than the %d any transport carries", len(clipped.Body), push.MaxBody)
	}
	if !utf8.ValidString(clipped.Body) {
		t.Error("the clip cut a rune in half, which arrives at the phone as a broken character")
	}
	if short := message(); short.Body != short.Clip().Body {
		t.Errorf("the clip moved a body that needed no clipping: %q became %q", short.Body, short.Clip().Body)
	}
}

// Message.Validate is what no adapter is asked to guess: a send that tells
// nobody anything, a collapse key no provider will hold, a TTL that runs
// backwards, a link that is not a link.
func TestASendThatTellsNobodyAnythingIsRefusedByThePort(t *testing.T) {
	empty := message()
	empty.Title, empty.Body = "", ""
	cases := []struct {
		what string
		m    push.Message
	}{
		{"neither a title nor a body", empty},
		{"a tag longer than the port's ceiling", tag(push.Message{Body: "x"}, push.MaxTag+1)},
		{"a TTL that runs backwards", ttl(push.Message{Body: "x"}, -time.Second)},
		{"a link that is not a URL", link("http://ex ample/")},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			if err := c.m.Validate(); err == nil {
				t.Errorf("a message with %s was accepted", c.what)
			}
		})
	}
	if err := message().Validate(); err != nil {
		t.Errorf("the port refuses the message every suite sends: %v", err)
	}
}

// An unknown platform never reaches a store: the CHECK constraint of the device
// table and the router's map are the same list, and a spelling off it is refused
// at the door.
func TestAPlatformOffTheListIsRefusedBeforeItReachesAStore(t *testing.T) {
	for _, p := range push.Platforms {
		if err := p.Validate(); err != nil {
			t.Errorf("the port refuses its own platform %q: %v", p, err)
		}
	}
	for _, unknown := range []push.Platform{"", "Android", "WEB", "sms"} {
		if err := unknown.Validate(); err == nil {
			t.Errorf("%q is not a platform and was accepted", unknown)
		}
		if _, err := (push.Adapters{}).Send(t.Context(), device(unknown), message()); err == nil {
			t.Errorf("a router carried a message to %q", unknown)
		}
	}
}

func message() push.Message {
	m := pushtest.Message()
	m.Tag = uuid.NewString()
	return m
}

func device(p push.Platform) push.Device {
	return push.Device{
		ID:       uuid.MustParse("6c6c6c6c-6c6c-6c6c-6c6c-6c6c6c6c6c6c"),
		TenantID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		UserID:   uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Platform: p, Token: "a-token",
	}
}

func tag(m push.Message, n int) push.Message           { m.Tag = strings.Repeat("t", n); return m }
func ttl(m push.Message, d time.Duration) push.Message { m.TTL = d; return m }
func link(u string) push.Message                       { m := message(); m.URL = u; return m }

func isGone(err error) bool { return strings.Contains(err.Error(), "device is gone") }
