package inference_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/inference/inferencetest"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTheConformanceFakeMatchesTheShippedScript(t *testing.T) {
	previousTotal := scriptTotal
	t.Cleanup(func() { scriptTotal = previousTotal })
	usage := inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}
	cfg := inference.Config{Model: inference.PracticeModel, DailyCalls: 3, Counter: limit.Memory()}
	shipped, err := inference.New(cfg, inference.NewScript(inference.Reply{Text: "a cat", Usage: usage}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = shipped.Close() })
	cfg.Counter = limit.Memory()
	leg := scriptLeg(t)
	underTest := leg.Build(t, inferencetest.End{Answers: []inference.Response{{Text: "a cat", Usage: usage}}}, cfg)
	ctx := tenancy.WithTenant(t.Context(), tenant)
	ask := inference.Request{Prompt: "what is this?"}
	want, err := shipped.Chat(ctx, ask)
	if err != nil {
		t.Fatal(err)
	}
	got, err := underTest.Chat(ctx, ask)
	if err != nil || got != want {
		t.Errorf("conformance fake = %+v, %v; shipped Script = %+v; the suite must run the fake consumers receive", got, err, want)
	}
}
