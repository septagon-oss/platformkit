package jobs_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
	"github.com/septagon-oss/platformkit/kit/jobs"
)

func ExampleJob() {
	items := []string{"read", "review"}
	runs := 0
	job := jobs.Job{
		Name: "example-uppercase", Every: time.Hour,
		Run: func(ctx context.Context, _ *db.Conn) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			for i, item := range items {
				items[i] = strings.ToUpper(item)
			}
			runs++
			return nil
		},
	}
	if err := jobs.Valid(job); err != nil {
		panic(err)
	}
	// The callback needs no database. Direct invocation does not acquire scheduler locks.
	if err := job.Run(context.Background(), nil); err != nil {
		panic(err)
	}
	fmt.Println(strings.Join(items, " "))
	fmt.Println("runs:", runs)
	// Output:
	// READ REVIEW
	// runs: 1
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExampleJob")
}
