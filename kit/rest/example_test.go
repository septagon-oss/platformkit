package rest_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/exampletest"
	"github.com/septagon-oss/platformkit/kit/rest"
)

func ExampleSpec_Mount() {
	api, _ := patchDocumentAPI()
	defer func() {
		if err := api.Cache().Close(); err != nil {
			panic(err)
		}
	}()
	s := spec
	s.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
	s.Mount(api.Surfaces(s.Module))
	if err := api.ValidateDeclarations(); err != nil {
		panic(err)
	}
	resource := api.Resources()[0]
	fmt.Println(resource.Module, resource.Entity, resource.Schema.Path)
	for _, op := range api.Recorded() {
		if op.OperationID == "tasks-task-list" {
			fmt.Println(op.Method, op.Path)
		}
	}
	// Output:
	// tasks task /api/v1/tasks/task
	// GET /api/v1/tasks/task
}

func ExampleCommand() {
	api, _ := patchDocumentAPI()
	defer func() {
		if err := api.Cache().Close(); err != nil {
			panic(err)
		}
	}()
	s := spec
	s.Operations = []httpx.CRUD{httpx.CRUDList, httpx.CRUDRead}
	surfaces := api.Surfaces(s.Module)
	s.Mount(surfaces)
	// This read-only callback is registered here; a wired request supplies its Tx.
	rest.Command(surfaces, s, "inspect", "Inspect a task", "", nil,
		func(_ context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*Task, error) {
			return crud.Get[*Task](tx, id)
		}, rest.CommandOptions{})
	if err := api.ValidateDeclarations(); err != nil {
		panic(err)
	}
	for _, op := range api.Recorded() {
		if op.OperationID == "tasks-task-inspect" {
			fmt.Println(op.Method, op.Path)
		}
	}
	command := api.Resources()[0].Commands[0]
	fmt.Println(command.Verb, command.Auth.String())
	// Output:
	// POST /api/v1/tasks/task/{id}/inspect
	// inspect permission task:write
}

func TestDocumentedExamplesExecute(t *testing.T) {
	exampletest.Execute(t, "ExampleSpec_Mount", "ExampleCommand")
}
