package subject_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/modules/subject"
)

func TestPersonalDataExportCanRunInTheWorker(t *testing.T) {
	manifest := subject.New()
	// The worker only discovers work declared by the manifest. A synchronous
	// service alone cannot produce and deliver an export after the request ends.
	if len(manifest.Jobs) == 0 && len(manifest.Subscriptions) == 0 {
		t.Fatal("subject declares no job or subscription: the worker cannot build, store, and notify a person's export")
	}
	for _, job := range manifest.Jobs {
		if err := jobs.Valid(job); err != nil {
			t.Errorf("export worker declaration: %v", err)
		}
	}
}

func TestPersonalDataRequestsHaveAnEntryPoint(t *testing.T) {
	if subject.New().Routes == nil {
		t.Fatal("subject mounts no routes: a person cannot request an export or confirm an erasure")
	}
}
