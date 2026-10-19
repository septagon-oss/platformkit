package inference_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfiguredProvidersCanRunThePortSuite(t *testing.T) {
	for _, endpoint := range []string{"local vision endpoint", "configured model"} {
		t.Run(endpoint, func(t *testing.T) {
			var sent atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				sent.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"model":"conformance","choices":[{"message":{"content":"a cat"},"finish_reason":"stop"}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`)
			}))
			t.Cleanup(server.Close)
			url, model := server.URL, "conformance"
			selection := "^TestTheConfiguredProvidersConform$"
			if endpoint == "configured model" {
				url, model = "https://endpoint.invalid", "operator-chosen-model"
				selection += "/the_endpoint_named_fixture$/an_engine_names_the_model_it_speaks_to$"
			}
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "test", "./kit/inference", "-count=1", "-v", "-run", selection)
			cmd.Dir = "../.."
			for _, entry := range os.Environ() {
				if !strings.HasPrefix(entry, "PLATFORMKIT_TEST_INFERENCE_") {
					cmd.Env = append(cmd.Env, entry)
				}
			}
			cmd.Env = append(cmd.Env, "PLATFORMKIT_TEST_INFERENCE_PROVIDERS=fixture",
				"PLATFORMKIT_TEST_INFERENCE_FIXTURE_URL="+url,
				"PLATFORMKIT_TEST_INFERENCE_FIXTURE_MODEL="+model,
				"PLATFORMKIT_TEST_INFERENCE_FIXTURE_KEY=test-key")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("configured provider's applicable conformance cases failed: %v\n%s", err, out)
			}
			if endpoint == "local vision endpoint" && sent.Load() == 0 {
				t.Fatal("the local endpoint's suite made no calls")
			}
		})
	}
}
