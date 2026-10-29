package inferencetest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
)

// C-1. An ask with no question is refused and the far end sees nothing. A wrong
// adapter counts the attempt, dials, and returns the vendor's 400 as if it were
// an outage — a person's empty form costs the tenant a call.
func emptyAsk(t *testing.T, fresh Legs) {
	engine, _, seen := open(t, fresh, End{})
	if _, err := engine.Chat(askCtx(t), inference.Request{Instruction: "name what is drawn"}); !errors.Is(err, inference.ErrAskShape) {
		t.Errorf("Chat(empty prompt) = %v, want ErrAskShape", err)
	}
	if got := seen.Asked(t); got != 0 {
		t.Errorf("the far end saw %d ask(s); a refused ask was sent, and counted", got)
	}
}

// C-3. A picture arrives as one image part whose decoded bytes are byte-identical,
// in an ask of exactly two content parts. A wrong adapter posts raw PNG bytes as
// text, names the data URL's wrong MIME type, or puts the picture in the system
// role — every one of which passes a unit test over the Go value.
func pictureBytes(t *testing.T, fresh Legs) {
	engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
	if _, err := engine.Chat(askCtx(t), picture); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	sent := seen.Sent(t)
	if len(sent) != 1 {
		t.Fatalf("the far end saw %d ask(s), want one", len(sent))
	}
	if string(sent[0].Image) != string(picture.Image) {
		t.Errorf("the far end recovered %d bytes, not the %d it was sent", len(sent[0].Image), len(picture.Image))
	}
	if sent[0].Parts != 2 {
		t.Errorf("the far end saw %d content part(s); an ask with a picture is a text part and an image part", sent[0].Parts)
	}
	if sent[0].Instruction != picture.Instruction {
		t.Errorf("the steer arrived as %q; a steer that does not arrive is a rule nobody sent", sent[0].Instruction)
	}
}

// C-4. Bytes that are not a PNG are refused by name and nothing is sent. A wrong
// adapter forwards them and takes the vendor's 400 as an outage, which turns a
// person's unsupported upload into a retry.
func imageFormat(t *testing.T, fresh Legs) {
	engine, _, seen := open(t, fresh, End{})
	jpeg := append([]byte{0xff, 0xd8, 0xff, 0xe0}, []byte("not a png, only a claim of one")...)
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?", Image: jpeg}); !errors.Is(err, inference.ErrImageFormat) {
		t.Errorf("Chat(jpeg) = %v, want ErrImageFormat", err)
	}
	if got := seen.Asked(t); got != 0 {
		t.Errorf("the far end saw %d ask(s); an ask this kit refuses must not be paid for", got)
	}
}

// C-5. A picture at the bound is refused and one byte under it is sent. The
// wrong adapter is an off-by-one that drops real pictures, or a bound nobody
// read at all.
func imageBound(t *testing.T, fresh Legs) {
	engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "ok", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
	over := samplePNG(1, 1)
	over = append(append([]byte(nil), over...), make([]byte, inference.MaxImageBytes-len(over)+1)...)
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?", Image: over}); !errors.Is(err, inference.ErrImageTooLarge) {
		t.Errorf("Chat(over the bound) = %v, want ErrImageTooLarge", err)
	}
	under := make([]byte, inference.MaxImageBytes-1)
	copy(under, samplePNG(1, 1))
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?", Image: under}); err != nil &&
		!errors.Is(err, inference.ErrProviderUnavailable) && !errors.Is(err, inference.ErrAnswerEmpty) {
		t.Errorf("Chat(one byte under the bound) = %v; a bound nobody read is a bound that refuses real pictures", err)
	}
}

// C-6. A prompt over the bound is refused before the counter. A wrong adapter
// bills a tenant for a document and reads the whole body of the answer it gets
// back for it.
func promptBound(t *testing.T, fresh Legs) {
	engine, store, _ := open(t, fresh, End{})
	long := make([]byte, inference.MaxPromptBytes+1)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: string(long)}); !errors.Is(err, inference.ErrAskTooLarge) {
		t.Errorf("Chat(long prompt) = %v, want ErrAskTooLarge", err)
	}
	if store.reads != 0 {
		t.Errorf("the allowance was read %d time(s) for an ask that was never allowed to be made", store.reads)
	}
}
