package source

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/dave/dst"
	"github.com/dave/dst/decorator"
	"golang.org/x/tools/go/packages"
)

const capturePackage = "github.com/septagon-oss/platformkit/ui/components/examples"

// rewrite changes existing direct string literals at one typed capture call.
// Its caller owns source identity, semantic proposal validation and persistence.
func rewrite(ctx context.Context, dir, filename string, before []byte, line, column int, props json.RawMessage, buildFlags ...string) ([]byte, error) {
	patch, err := stringPatch(props)
	if err != nil {
		return nil, err
	}
	if line < 1 || column < 0 {
		return nil, fmt.Errorf("source position requires a positive line and nonnegative column")
	}
	if !filepath.IsAbs(filename) {
		filename = filepath.Join(dir, filename)
	}
	filename, err = filepath.Abs(filename)
	if err != nil {
		return nil, fmt.Errorf("source filename: %w", err)
	}
	pkg, file, err := loadSource(ctx, filename, before, buildFlags...)
	if err != nil {
		return nil, err
	}
	var matches []*ast.CallExpr
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok {
			position := pkg.Fset.PositionFor(call.Pos(), false)
			if position.Line == line && (column == 0 || position.Column == column) && isCapture(pkg, call) {
				matches = append(matches, call)
			}
		}
		return true
	})
	if len(matches) != 1 {
		return nil, fmt.Errorf("source position %d:%d selects %d typed capture calls; require exactly one", line, column, len(matches))
	}
	call := matches[0]
	if len(call.Args) < 2 {
		return nil, fmt.Errorf("capture has no direct Props argument")
	}
	literal, ok := call.Args[1].(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("Props must be a direct keyed struct literal")
	}
	structure, ok := pkg.TypesInfo.TypeOf(literal).Underlying().(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("Props must have a struct type")
	}
	edits := make(map[*ast.BasicLit]string, len(patch))
	for name, value := range patch {
		field, err := patchField(structure, name)
		if err != nil {
			return nil, err
		}
		var target *ast.BasicLit
		for _, entry := range literal.Elts {
			keyed, ok := entry.(*ast.KeyValueExpr)
			if !ok {
				return nil, fmt.Errorf("Props must use keyed fields")
			}
			key, ok := keyed.Key.(*ast.Ident)
			if ok && key.Name == field {
				target, _ = keyed.Value.(*ast.BasicLit)
			}
		}
		if target == nil || target.Kind != token.STRING {
			return nil, fmt.Errorf("property %q requires an existing direct string literal; missing or computed values cannot be rewritten", name)
		}
		edits[target] = value
	}
	dec := decorator.NewDecorator(pkg.Fset)
	tree, err := dec.DecorateFile(file)
	if err != nil {
		return nil, fmt.Errorf("decorate source: %w", err)
	}
	for target, value := range edits {
		dec.Dst.Nodes[target].(*dst.BasicLit).Value = strconv.Quote(value)
	}
	var output bytes.Buffer
	if err := decorator.NewRestorer().Fprint(&output, tree); err != nil {
		return nil, fmt.Errorf("restore source: %w", err)
	}
	if _, _, err := loadSource(ctx, filename, output.Bytes(), buildFlags...); err != nil {
		return nil, fmt.Errorf("rewritten source: %w", err)
	}
	return output.Bytes(), nil
}

func loadSource(ctx context.Context, filename string, content []byte, buildFlags ...string) (*packages.Package, *ast.File, error) {
	loaded, err := packages.Load(&packages.Config{
		Context: ctx, Dir: filepath.Dir(filename), Mode: packages.LoadSyntax,
		Env:        append(os.Environ(), "GOWORK=off", "GOFLAGS=", "CGO_ENABLED=0"),
		BuildFlags: append([]string{"-mod=readonly"}, buildFlags...),
		Overlay:    map[string][]byte{filename: content},
	}, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("load source package: %w", err)
	}
	if len(loaded) != 1 {
		return nil, nil, fmt.Errorf("source package resolved to %d packages; require exactly one", len(loaded))
	}
	pkg := loaded[0]
	if len(pkg.Errors) != 0 || pkg.IllTyped {
		return nil, nil, fmt.Errorf("source package does not type-check: %v", pkg.Errors)
	}
	for i, compiled := range pkg.CompiledGoFiles {
		if compiled == filename {
			return pkg, pkg.Syntax[i], nil
		}
	}
	return nil, nil, fmt.Errorf("source file is not compiled in the selected package")
}

func isCapture(pkg *packages.Package, call *ast.CallExpr) bool {
	function := call.Fun
	switch indexed := function.(type) {
	case *ast.IndexExpr:
		function = indexed.X
	case *ast.IndexListExpr:
		function = indexed.X
	}
	var identifier *ast.Ident
	switch named := function.(type) {
	case *ast.Ident:
		identifier = named
	case *ast.SelectorExpr:
		identifier = named.Sel
	}
	fn, ok := pkg.TypesInfo.Uses[identifier].(*types.Func)
	if !ok || fn.Pkg() == nil || fn.Pkg().Path() != capturePackage {
		return false
	}
	switch fn.Name() {
	case "ExampleOf", "ExampleWithChildren", "ExampleWithSlots":
		return true
	default:
		return false
	}
}

func patchField(structure *types.Struct, name string) (string, error) {
	var found *types.Var
	for i := range structure.NumFields() {
		field := structure.Field(i)
		tag := reflect.StructTag(structure.Tag(i))
		if !field.Exported() || field.Embedded() || tag.Get("delivery") == "internal" {
			continue
		}
		jsonName, _, _ := strings.Cut(tag.Get("json"), ",")
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			jsonName = field.Name()
		}
		if jsonName == name {
			if found != nil {
				return "", fmt.Errorf("property %q has ambiguous Go fields", name)
			}
			found = field
		}
	}
	if found == nil {
		return "", fmt.Errorf("property %q is not a direct exported JSON field", name)
	}
	basic, ok := found.Type().Underlying().(*types.Basic)
	if !ok || basic.Kind() != types.String {
		return "", fmt.Errorf("property %q is not a string field", name)
	}
	return found.Name(), nil
}

func stringPatch(raw json.RawMessage) (map[string]string, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("props must be a valid JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	start, _ := decoder.Token()
	if start != json.Delim('{') {
		return nil, fmt.Errorf("props must be a JSON object")
	}
	patch := map[string]string{}
	for decoder.More() {
		key, _ := decoder.Token()
		name := key.(string)
		if _, duplicate := patch[name]; duplicate {
			return nil, fmt.Errorf("props repeats property %q", name)
		}
		token, _ := decoder.Token()
		value, ok := token.(string)
		if !ok {
			return nil, fmt.Errorf("property %q requires a JSON string", name)
		}
		patch[name] = value
	}
	if len(patch) == 0 {
		return nil, fmt.Errorf("props must contain at least one property")
	}
	return patch, nil
}
