package inference_test

import (
	"context"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/inference/inferencetest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestProviderMetadataDoesNotExposeThePrompt(t *testing.T) {
	const prompt = "private drawing belonging to my daughter"
	for _, field := range []string{"model", "finish reason"} {
		t.Run(field, func(t *testing.T) {
			previousTotal := openaiTotal
			t.Cleanup(func() { openaiTotal = previousTotal })
			recorder := tracetest.NewSpanRecorder()
			tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			previous := otel.GetTracerProvider()
			otel.SetTracerProvider(tracer)
			t.Cleanup(func() {
				otel.SetTracerProvider(previous)
				_ = tracer.Shutdown(context.WithoutCancel(t.Context()))
			})
			response := inference.Response{Text: "a cat", Model: "vendor-model", Finish: "stop", Usage: inference.Usage{InputTokens: 7, OutputTokens: 3, Reported: true}}
			if field == "model" {
				response.Model = prompt
			} else {
				response.Finish = prompt
			}
			ctx := tenancy.WithTenant(t.Context(), tenant)
			engine, _ := open(t, openaiLeg, inferencetest.End{Answers: []inference.Response{response}}, ctx)
			got, err := engine.Chat(ctx, inference.Request{Prompt: prompt})
			if err != nil && got != (inference.Answer{}) {
				t.Errorf("a refusal returned an answer: %+v", got)
			}
			if err != nil && strings.Contains(err.Error(), prompt) {
				t.Errorf("provider-controlled %s exposed the prompt in the error: %v", field, err)
			}
			spans := recorder.Ended()
			if len(spans) != 1 {
				t.Fatalf("wire call recorded %d spans; want one", len(spans))
			}
			for _, attr := range spans[0].Attributes() {
				if strings.Contains(attr.Value.Emit(), prompt) {
					t.Errorf("provider-controlled %s exposed the prompt in %s = %q", field, attr.Key, attr.Value.Emit())
				}
			}
			if len(spans[0].Events()) != 0 {
				t.Error("the inference span contains events")
			}
		})
	}
}
