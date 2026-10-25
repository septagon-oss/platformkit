package inferencetest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
)

// C-2. A text-only ask round-trips: the text, the model name and the usage the
// endpoint reported. A wrong adapter loses the model name, and then no span can
// tell two endpoints apart.
func textAnswer(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	engine, _, seen := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat", Model: "far-model",
		Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
	answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if answer.Text != "a cat" {
		t.Errorf("answer %q, want the text the far end said", answer.Text)
	}
	if answer.Model != "far-model" {
		t.Errorf("model %q, want the name the far end gave it", answer.Model)
	}
	if !answer.Usage.Reported || answer.Usage.InputTokens != 7 || answer.Usage.OutputTokens != 3 {
		t.Errorf("usage %+v, want the counts the far end reported", answer.Usage)
	}
	sent := seen.Sent(t)
	if len(sent) != 1 || sent[0].Prompt != "what is this?" {
		t.Fatalf("the far end saw %+v, want the one prompt", sent)
	}
}

// C-7. An answer of exactly the bound is returned; one byte more is refused and
// returns no text at all. A wrong adapter truncates happily and stores half a
// translation, which is the data corruption no caller can detect afterwards.
func answerBound(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'x'
	}
	for _, tc := range []struct {
		text    string
		wantErr bool
	}{{string(long[:64]), false}, {string(long), true}} {
		engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: tc.text, Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}}})
		answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
		if tc.wantErr && !errors.Is(err, inference.ErrAnswerTooLarge) {
			t.Errorf("Chat(answer %d bytes) = %v, want ErrAnswerTooLarge", len(tc.text), err)
		}
		if tc.wantErr && answer.Text != "" {
			t.Errorf("a refused answer still carried %d bytes of text", len(answer.Text))
		}
		if !tc.wantErr && err != nil {
			t.Errorf("Chat(answer at the bound) = %v, want it returned", err)
		}
	}
}

// C-8. A far end that says it cut the answer short is refused whole-and-nothing.
// A wrong adapter hands back a truncated guess as if the model had finished, and
// the caller has nothing to detect that with.
func incompleteAnswer(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat sit", Finish: "length"}}})
	answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
	if !errors.Is(err, inference.ErrAnswerIncomplete) {
		t.Errorf("Chat(finish=length) = %v, want ErrAnswerIncomplete", err)
	}
	if answer.Text != "" {
		t.Errorf("a truncated answer was returned as %q", answer.Text)
	}
}

// C-9. A 200 with nothing in it is refused. A wrong adapter returns "" with a nil
// error, which a caller reads as "the model said nothing" — the honest-looking
// wrong answer, and the one a person cannot tell from an empty drawing.
func emptyAnswer(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: ""}}})
	if _, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"}); !errors.Is(err, inference.ErrAnswerEmpty) {
		t.Errorf("Chat(no answer) = %v, want ErrAnswerEmpty", err)
	}
}

// C-10. Missing usage refuses the whole answer; no token count is invented.
func reportedUsage(t *testing.T, fresh Legs, leg Leg) {
	deviates(t, leg)
	engine, _, _ := open(t, fresh, End{Answers: []inference.Response{{Text: "a cat"}}})
	answer, err := engine.Chat(askCtx(t), inference.Request{Prompt: "what is this?"})
	if !errors.Is(err, inference.ErrAnswerShape) || answer != (inference.Answer{}) {
		t.Errorf("unaccounted answer = %+v, %v; want no answer and ErrAnswerShape", answer, err)
	}
}
