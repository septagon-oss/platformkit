package main

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/seed"
)

func TestDemoSeedIncludesAssignedWorkAndAnImage(t *testing.T) {
	docs, err := seed.Load(seedFiles, "seed", "demo")
	if err != nil {
		t.Fatal(err)
	}
	var tasks, images int
	for _, doc := range docs {
		if doc.Resource == "tasks" {
			tasks += len(doc.Records)
		}
		for _, record := range doc.Records {
			if record.Asset != "" {
				images++
			}
		}
	}
	if tasks == 0 || images == 0 {
		t.Errorf("demo seed declares %d tasks and %d embedded images; want both so the walkthrough has work and an image", tasks, images)
	}
}
