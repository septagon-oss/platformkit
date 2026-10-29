package internal

import "github.com/septagon-oss/platformkit/modules/file/contracts"

func libraryAnchor(name string) map[string]string {
	return map[string]string{"data-screen-anchor": string(contracts.Screen("library")) + "#" + name}
}
