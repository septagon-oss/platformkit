package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestBothE2EInstallationsDeclareAUsableWebhookKey(t *testing.T) {
	script, err := os.ReadFile("../../scripts/e2e.sh")
	if err != nil {
		t.Fatal(err)
	}
	keys := make(map[string]string)
	for _, writer := range []string{"write_config", "write_second_config"} {
		t.Run(writer, func(t *testing.T) {
			_, function, found := strings.Cut(string(script), "\n"+writer+"() {\n")
			if !found {
				t.Fatalf("configuration writer %s is absent", writer)
			}
			function, _, _ = strings.Cut(function, "\n}\n")
			_, body, found := strings.Cut(function, "<<YAML\n")
			if !found {
				t.Fatal("configuration writer has no YAML body")
			}
			body, _, found = strings.Cut(body, "\nYAML")
			if !found {
				t.Fatal("configuration writer has no YAML terminator")
			}
			_, body, found = strings.Cut(body, "\nwebhook:\n")
			if !found {
				t.Fatal("configuration writer declares no webhook configuration")
			}
			var declared struct {
				Webhook config.Webhook `yaml:"webhook"`
			}
			if err := yaml.Unmarshal([]byte("webhook:\n"+body), &declared); err != nil {
				t.Fatalf("configuration YAML: %v", err)
			}
			cfg := referenceCompositionConfig(t)
			cfg.Webhook = declared.Webhook
			if _, err := sentencesOf(cfg).Plan(pkit.Deployment{Environment: pkit.Development, Config: cfg}); err != nil {
				t.Fatalf("the installation's declared webhook configuration does not plan: %v", err)
			}
			keys[writer] = declared.Webhook.SecretKey
		})
	}
	if len(keys) == 2 && keys["write_config"] == keys["write_second_config"] {
		t.Error("independent installations share their wrapping key")
	}
}
