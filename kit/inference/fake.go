package inference

import (
	"context"
	"sync"
)

// Reply is one scripted answer. The zero Reply answers "" and so is refused
// ErrAnswerEmpty by the same rule a real endpoint is: a fake that let an empty
// answer through would hide the one bug a caller could otherwise only meet in
// production.
type Reply struct {
	Text  string
	Model string
	Usage Usage
	// Finish is the wire's own token. "" means "stop": a scripted answer is a
	// finished one unless the script says otherwise.
	Finish string
	// Err is answered instead of a text. It is how a consumer's test meets a
	// provider failure without running a server: "a provider failure is no guess
	// this tick" is a case about the caller, and it should not need an HTTP stack.
	Err error
}

// Script is a Provider that answers from a list, in order. It is the double every
// test of this port runs on, and the far end of the conformance suite's fake leg.
//
// It is deterministic in the three ways a test needs: no clock of its own beyond
// the caller's context, no randomness, and replies in order. The last reply is the
// answer to every later ask, because a test scripts the asks it asserts on and an
// installation in practice mode is a fake with one reply; a script that ran out
// mid-test would be a flake, and one that invented an answer past its list would
// be a lie. An empty script answers ErrNoScript and counts nothing: a test that
// under-scripts is told so rather than being handed "".
type Script struct {
	mu      sync.Mutex
	replies []Reply
	asks    []Request
	closed  bool
}

// NewScript returns the far end a test asserts on. Asks is what it saw.
func NewScript(replies ...Reply) *Script { return &Script{replies: replies} }

func (s *Script) Chat(ctx context.Context, ask Request) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return Response{}, ErrClosed
	}
	s.asks = append(s.asks, ask)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return Response{}, ctxErr
	}
	if len(s.replies) == 0 {
		return Response{}, ErrNoScript
	}
	reply := s.replies[0]
	if len(s.replies) > 1 {
		s.replies = s.replies[1:]
	}
	if reply.Err != nil {
		return Response{}, reply.Err
	}
	finish := reply.Finish
	if finish == "" {
		finish = "stop"
	}
	model := reply.Model
	if model == "" {
		model = PracticeModel
	}
	return Response{
		Text:      reply.Text,
		Model:     model,
		Finish:    finish,
		Truncated: finish == "length",
		Usage:     reply.Usage,
	}, nil
}

// Close is the second half of what an engine does to an adapter: a script closed
// under a Close answers ErrClosed to everything after, which is how the suite can
// tell "the engine stopped asking" from "the far end stopped answering".
func (s *Script) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.closed = true
	return nil
}

// Asks returns every Request the script was handed, in order.
//
// This is the assertion a consumer's test needs and cannot make any other way:
// that the steer went in Instruction and not in Prompt, and that the picture
// arrived as the bytes it was given. The consumer asserts it from outside this
// package, which is the point — the port never has to know who is checking.
func (s *Script) Asks() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.asks...)
}

// Practice is the engine of an installation with no endpoint configured, and the
// shipped default when one is not.
//
// It is the only uncapped path in this package, and it is uncapped provably:
// Practice builds the far end itself, so no outside wire can reach this
// constructor, and an engine with no counter has no dial. Its Model is
// PracticeModel, which is how an application answers "practice mode" in its own
// words without reading anyone's internals, and how a test proves the fake cannot
// be mistaken for a vendor. It uses the same engine and default bounds over a
// Script, bypassing only New's requirement for an allowance. Scripted replies
// still require reported usage before an answer can be returned.
func Practice(replies ...Reply) Engine {
	cfg, err := (Config{Model: PracticeModel}).Defaults()
	if err != nil {
		panic("inference: " + err.Error())
	}
	return &engine{cfg: cfg, provider: NewScript(replies...)}
}
