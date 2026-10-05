package admin_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/export"
)

func TestTheKernelGalleryPublishesACommandRecoveryExample(t *testing.T) {
	router := mount(t)
	status, body, _ := callAt(t, router, operatorHost, http.MethodGet, "/app/admin/_gallery/export", "")
	if status != http.StatusOK {
		t.Fatalf("gallery export returned %d: %s", status, body)
	}
	var snapshot export.DesignExport
	if err := json.Unmarshal([]byte(body), &snapshot); err != nil {
		t.Fatal(err)
	}
	for _, example := range snapshot.Examples {
		if strings.Contains(example.HTML, `hx-ext="command"`) && strings.Contains(example.HTML, "<form") {
			return
		}
	}
	t.Fatalf("kernel gallery publishes %d examples but none demonstrates the command controller", len(snapshot.Examples))
}
