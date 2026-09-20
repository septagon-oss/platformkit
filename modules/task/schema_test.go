package task_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/modules/task/contracts"
)

// TestTaskSchemaIsTheProjectedShape pins the JSON Schema kit/entity commits to
// the entity it is named after. That file is written out by hand — kit/entity
// is a leaf and importing task to describe a task would be a cycle, not a
// test — so this is the only thing that notices when a tag here has moved and
// the copy in the leaf has not. It writes nothing: the golden belongs to the
// leaf, which is where its UPDATE_GOLDEN run lives.
func TestTaskSchemaIsTheProjectedShape(t *testing.T) {
	t.Parallel()
	doc, err := entity.JSONSchema(crud.Fields[*contracts.Task]())
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	const leaf = "../../kit/entity/testdata/task.schema.json"
	want, err := os.ReadFile(leaf)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s no longer describes contracts.Task.\nRerun UPDATE_GOLDEN=1 go test ./kit/entity -run TestJSONSchemaGolden and read the diff.\n%s", leaf, got)
	}
}
