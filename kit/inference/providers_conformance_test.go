package inference_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/inference"
	"github.com/septagon-oss/platformkit/kit/inference/inferencetest"
	"github.com/septagon-oss/platformkit/kit/inference/providers/openai"
	"github.com/septagon-oss/platformkit/kit/limit"
)

// TestTheConfiguredProvidersConform runs the port's suite against the endpoints an
// operator named, which is the half of the portability claim a checkout with no
// network cannot make. `make inference-conformance PROVIDERS=ollama,hosted` is the
// door; it names each endpoint's address, model and key by environment variable.
//
// A named provider that cannot be reached fails the run rather than skipping it: an
// address a person wrote down is a promise. Each leg declares that it Honours the
// port's own semantics only, and prints the tier it declined to play, because a
// hosted vendor will not answer a truncated reply to order and a suite that read its
// silence as a pass would be reading nothing.
func TestTheConfiguredProvidersConform(t *testing.T) {
	named := os.Getenv("PLATFORMKIT_TEST_INFERENCE_PROVIDERS")
	if named == "" {
		t.Skip("no provider named: make inference-conformance PROVIDERS=<name>,<name>, each configured by " +
			"PLATFORMKIT_TEST_INFERENCE_<NAME>_URL, _MODEL and _KEY")
	}
	var legs []inferencetest.Legs
	for _, name := range strings.Split(named, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		legs = append(legs, providerLeg(t, name))
	}
	if len(legs) == 0 {
		t.Fatalf("%s named no provider this run could read", named)
	}
	inferencetest.Conformance(t, legs...)
}

// providerLeg is one endpoint, addressed as the operator wrote it. Nothing here
// prints a key: the configuration is read, named by variable and never echoed.
func providerLeg(t *testing.T, name string) inferencetest.Legs {
	return func(t *testing.T) inferencetest.Leg {
		upper := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		endpoint, model := os.Getenv("PLATFORMKIT_TEST_INFERENCE_"+upper+"_URL"), os.Getenv("PLATFORMKIT_TEST_INFERENCE_"+upper+"_MODEL")
		if endpoint == "" || model == "" {
			t.Fatalf("%s was named without PLATFORMKIT_TEST_INFERENCE_%s_URL or its _MODEL", name, upper)
		}
		key := os.Getenv("PLATFORMKIT_TEST_INFERENCE_" + upper + "_KEY")
		base, err := url.Parse(endpoint)
		if err != nil {
			t.Fatalf("%s has an invalid endpoint URL", name)
		}
		localHTTP := base.Scheme == "http" && (base.Hostname() == "localhost" || net.ParseIP(base.Hostname()).IsLoopback())
		var mu sync.Mutex
		var sent []inferencetest.Seen
		var asked, total int
		return inferencetest.Leg{
			Name:  "the endpoint named " + name,
			Model: model,
			Build: func(t *testing.T, end inferencetest.End, cfg inference.Config) inference.Engine {
				mu.Lock()
				sent, asked = nil, 0
				mu.Unlock()
				cfg.Model = model
				provider, err := openai.New(cfg, openai.Config{BaseURL: endpoint, APIKey: key, Insecure: localHTTP})
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				engine, err := inference.New(cfg, &metered{Provider: provider, mu: &mu, sent: &sent, asked: &asked, total: &total})
				if err != nil {
					t.Fatalf("%s: %v", name, err)
				}
				t.Cleanup(func() { engine.Close() })
				return engine
			},
			Counter: func(t *testing.T) inference.Counter { return limit.Memory() },
			Sent: func(t *testing.T) []inferencetest.Seen {
				mu.Lock()
				defer mu.Unlock()
				return append([]inferencetest.Seen(nil), sent...)
			},
			Asked: func(t *testing.T) int { mu.Lock(); defer mu.Unlock(); return asked },
			// An endpoint the operator named is the far end: it has no cue to
			// misbehave on and no outage to be made to answer with, so the legs
			// that need one are the suite's own, and this one says so.
			Uncountable: func(t *testing.T) inference.Counter { return unreachable{} },
			Total:       func(t *testing.T) int { mu.Lock(); defer mu.Unlock(); return total },
			Honours:     func(tier inferencetest.Tier) bool { return tier == inferencetest.PortSemantics },
		}
	}
}

// metered is what a leg with no handler of its own watches its own wire with: the
// ask counted on this side, and the answer's own status read rather than parsed.
type metered struct {
	inference.Provider
	mu    *sync.Mutex
	sent  *[]inferencetest.Seen
	asked *int
	total *int
}

func (m *metered) Chat(ctx context.Context, ask inference.Request) (inference.Response, error) {
	m.mu.Lock()
	*m.sent = append(*m.sent, inferencetest.Seen{
		Instruction: ask.Instruction, Prompt: ask.Prompt, Image: ask.Image,
		Parts: 1 + boolToInt(ask.Image != nil),
	})
	*m.asked++
	*m.total++
	m.mu.Unlock()
	return m.Provider.Chat(ctx, ask)
}
