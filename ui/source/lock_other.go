//go:build !unix || aix

package source

import "fmt"

func lock(string) (func(), error) {
	return nil, fmt.Errorf("source: applying source changes currently requires Unix advisory locks")
}
