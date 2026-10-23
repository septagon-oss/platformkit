package main

import (
	"testing"

	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
)

// TestTheReferenceProductDeclaresItsPrimaryAction checks the reference
// application's one declaration about its workspace home: the shell leads with
// the task list, for whoever may read tasks, and the plan carries it as written.
func TestTheReferenceProductDeclaresItsPrimaryAction(t *testing.T) {
	_, cfg := configure(t)
	c := compose(cfg)
	want := admincontracts.PrimaryAction{Label: "Go to tasks", Screen: "task/tasks", Permission: taskcontracts.PermissionTaskRead}
	if got := value[admincontracts.PrimaryAction](c.planned); got != want {
		t.Errorf("the reference product's primary action = %+v, want %+v", got, want)
	}
}
