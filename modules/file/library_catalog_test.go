package file_test

import "testing"

func TestFilesAndCaptionsAreAvailableToCatalogClients(t *testing.T) {
	_, _, _, _, _, api := libraryFixtureAPI(t)
	for _, resource := range api.Resources() {
		if resource.Module != "file" {
			continue
		}
		for _, command := range resource.Commands {
			if command.Verb == "alt" && command.Run != nil {
				return
			}
		}
		t.Fatal("the file catalog resource has no executable caption command")
	}
	t.Fatal("the library mounts a file page and a caption command, but registers no file resource for catalog clients")
}
