// Package contracts owns inert picture declarations and the source port.
package contracts

import "github.com/septagon-oss/platformkit/kit/picture"

// Pure value aliases preserve the module contract through the shared compiler.
type Template = picture.Template
type Templates = picture.Templates
type Part = picture.Part
type Selection = picture.Selection
type Value = picture.Value
type Resolved = picture.Resolved

// Compile validates bounded UTF-8 XML; refusals return no usable declaration.
func Compile(name, source string) (Template, error) { return picture.Compile(name, source) }

// Resolve validates a complete value and returns detached canonical inputs.
func Resolve(t Template, v Value) (Resolved, error) { return picture.Resolve(t, v) }

// Compose serializes validated inputs, returning no partial output on failure.
func Compose(t Template, r Resolved) (string, error) { return picture.Compose(t, r) }
