// Package main — scaffold.go owns `platformkit new`, the single command that
// turns the kit into YOUR product.
//
//   - `new app <name>` writes a downstream Go application: a main that boots
//     the nine-module starter through starterapp.Run, an additive module
//     registry, a fail-closed config, a container image, a Makefile, and an
//     agent pack (AGENTS.md / llms.txt) so an AI coding agent can extend the
//     app the same way a person would.
//   - `new module <name>` writes one tenant-scoped module — contract, store,
//     migration, HTTP surface, and a test — mirroring the reference module in
//     pk-apps/reference/custommodule exactly, so generated code is code the
//     conformance and verify gates already cover.
//
// Design rules:
//   - Generated modules are ADDITIVE: each registers itself in an init(), so
//     `new module` never edits an existing file. No codegen surgery, no merge
//     conflicts on regeneration.
//   - The generated go.mod pins the SAME pk-apps the running CLI was built
//     against (read from the build info), so a scaffold uses the exact,
//     tested module set — not a floating @latest that may not compose.
//   - Every generated file is data-driven from one template set embedded in
//     the binary; the tests render the whole set and compile it.
//
// Implements: REQ-016.
// Per: ADR-0017, ADR-0029.
// Discipline: C-14.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"text/template"
	"unicode"

	"github.com/spf13/cobra"
)

//go:embed templates
var scaffoldTemplates embed.FS

// pinnedDep reports the version the running binary was built against for the
// given module path, or "" when it cannot be determined (e.g. `go run` of an
// unversioned checkout). Callers fall back to resolving @latest via tidy.
func pinnedDep(path string) string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range bi.Deps {
		if dep.Path == path {
			// A replace directive means the recorded version is not resolvable
			// from the proxy; let tidy sort it out.
			if dep.Replace != nil {
				return ""
			}
			return dep.Version
		}
	}
	return ""
}

// appData drives the `new app` templates.
type appData struct {
	Name          string // display + directory name, e.g. "acme"
	Module        string // go module path, e.g. "github.com/acme/acme"
	AppsVersion   string // pinned pk-apps version, or "" to resolve via tidy
	SQLiteVersion string // pinned modernc.org/sqlite version, or ""
}

// moduleData drives the `new module` templates. Every identifier is derived
// from one singular name so the generated names never disagree.
type moduleData struct {
	Package  string // the app's go module path, for imports in tests
	Singular string // "invoice"
	Plural   string // "invoices"
	Struct   string // "Invoice"
	Var      string // "invoice" (Go identifier, lower)
	Scope    string // "invoices" (api-key scope prefix)
	Table    string // "invoices"
	Route    string // "/api/v1/invoices"
}

// sanitizeName lowercases and keeps [a-z0-9_], collapsing anything else to a
// single underscore, so a display name becomes a safe directory / identifier
// stem. It never returns a leading digit.
func sanitizeName(raw string) string {
	var b strings.Builder
	prevUnderscore := false
	for _, r := range strings.ToLower(strings.TrimSpace(raw)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			if !prevUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return ""
	}
	if out[0] >= '0' && out[0] <= '9' {
		out = "app_" + out
	}
	return out
}

// goIdentifier turns a sanitized stem into a lowerCamel Go identifier, and
// titleCase gives the exported form.
func titleCase(s string) string {
	parts := strings.Split(s, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

func lowerIdentifier(s string) string {
	t := titleCase(s)
	if t == "" {
		return ""
	}
	r := []rune(t)
	r[0] = unicode.ToLower(r[0])
	return string(r)
}

// pluralize is deliberately naive — append "s", or "es" after s/x/z/ch/sh.
// A module author who wants an irregular plural edits the generated file;
// scaffolding is a starting point, not a linguistics engine.
func pluralize(s string) string {
	switch {
	case strings.HasSuffix(s, "s"), strings.HasSuffix(s, "x"), strings.HasSuffix(s, "z"),
		strings.HasSuffix(s, "ch"), strings.HasSuffix(s, "sh"):
		return s + "es"
	case strings.HasSuffix(s, "y") && len(s) > 1 && !isVowel(rune(s[len(s)-2])):
		return s[:len(s)-1] + "ies"
	default:
		return s + "s"
	}
}

func isVowel(r rune) bool { return strings.ContainsRune("aeiou", r) }

// renderTemplate executes one embedded template into dst, creating parent
// directories. It refuses to overwrite unless force is set.
func renderTemplate(tmplPath, dst string, data any, force bool) error {
	if !force {
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", dst)
		}
	}
	raw, err := scaffoldTemplates.ReadFile(tmplPath)
	if err != nil {
		return err
	}
	t, err := template.New(filepath.Base(tmplPath)).Parse(string(raw))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	return t.Execute(f, data)
}

// appFileMap lists the app templates and their destination names. Keys are
// paths under the embedded templates/app dir; values are the on-disk names
// (dotfiles are stored as gitignore.tmpl etc. so go:embed does not skip them).
var appFileMap = map[string]string{
	"templates/app/go.mod.tmpl":              "go.mod",
	"templates/app/main.go.tmpl":             "main.go",
	"templates/app/modules.go.tmpl":          "modules.go",
	"templates/app/config.example.yaml.tmpl": "config.example.yaml",
	"templates/app/Dockerfile.tmpl":          "Dockerfile",
	"templates/app/compose.yaml.tmpl":        "compose.yaml",
	"templates/app/Makefile.tmpl":            "Makefile",
	"templates/app/gitignore.tmpl":           ".gitignore",
	"templates/app/README.md.tmpl":           "README.md",
	"templates/app/AGENTS.md.tmpl":           "AGENTS.md",
	"templates/app/llms.txt.tmpl":            "llms.txt",
}

func newAppData(name, modulePath string) (appData, error) {
	stem := sanitizeName(name)
	if stem == "" {
		return appData{}, fmt.Errorf("app name %q has no usable characters", name)
	}
	if modulePath == "" {
		modulePath = stem
	}
	return appData{
		Name:          stem,
		Module:        modulePath,
		AppsVersion:   pinnedDep("github.com/septagon-oss/pk-apps"),
		SQLiteVersion: pinnedDep("modernc.org/sqlite"),
	}, nil
}

func generateApp(dir string, data appData, force bool) ([]string, error) {
	var written []string
	// Deterministic order for stable output and tests.
	order := []string{
		"templates/app/go.mod.tmpl", "templates/app/main.go.tmpl",
		"templates/app/modules.go.tmpl", "templates/app/config.example.yaml.tmpl",
		"templates/app/Dockerfile.tmpl", "templates/app/compose.yaml.tmpl",
		"templates/app/Makefile.tmpl", "templates/app/gitignore.tmpl",
		"templates/app/README.md.tmpl", "templates/app/AGENTS.md.tmpl",
		"templates/app/llms.txt.tmpl",
	}
	for _, tmpl := range order {
		dst := filepath.Join(dir, appFileMap[tmpl])
		if err := renderTemplate(tmpl, dst, data, force); err != nil {
			return written, err
		}
		written = append(written, dst)
	}
	return written, nil
}

func newModuleData(pkg, name string) (moduleData, error) {
	singular := sanitizeName(name)
	if singular == "" {
		return moduleData{}, fmt.Errorf("module name %q has no usable characters", name)
	}
	plural := pluralize(singular)
	return moduleData{
		Package:  pkg,
		Singular: singular,
		Plural:   plural,
		Struct:   titleCase(singular),
		Var:      lowerIdentifier(singular),
		Scope:    plural,
		Table:    plural,
		Route:    "/api/v1/" + plural,
	}, nil
}

func generateModule(dir string, data moduleData, force bool) ([]string, error) {
	var written []string
	files := []struct{ tmpl, dst string }{
		{"templates/module/mod.go.tmpl", "mod_" + data.Singular + ".go"},
		{"templates/module/mod_test.go.tmpl", "mod_" + data.Singular + "_test.go"},
		{"templates/module/migration.up.sql.tmpl",
			filepath.Join("migrations", data.Singular, "0001_create_"+data.Table+".up.sql")},
	}
	for _, f := range files {
		dst := filepath.Join(dir, f.dst)
		if err := renderTemplate(f.tmpl, dst, data, force); err != nil {
			return written, err
		}
		written = append(written, dst)
	}
	return written, nil
}

// moduleGoPath reads the go.mod in dir and returns its module path, so a
// generated module's test can import the app package. Empty when dir is not a
// module root (the module still generates; only the test's import is a stub).
func moduleGoPath(dir string) string {
	raw, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// runGoModTidy resolves the generated dependency graph in dir. Best-effort:
// on a machine without `go` or without network it fails, and the caller tells
// the operator to run it themselves — the scaffold is still complete.
func runGoModTidy(dir string) error {
	if _, err := exec.LookPath("go"); err != nil {
		return fmt.Errorf("go toolchain not found on PATH")
	}
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}
	return nil
}

func newNewCmd() *cobra.Command {
	newCmd := &cobra.Command{
		Use:   "new",
		Short: "Scaffold a new app or module",
		Long: "Scaffold your product from the kit. `new app` writes a downstream " +
			"application; `new module` writes a tenant-scoped module into it.",
	}
	newCmd.AddCommand(newNewAppCmd(), newNewModuleCmd())
	return newCmd
}

func newNewAppCmd() *cobra.Command {
	var modulePath string
	var force, skipTidy bool
	cmd := &cobra.Command{
		Use:   "app <name>",
		Short: "Scaffold a downstream PlatformKit application",
		Long: "Write a new Go application that boots the nine-module starter and " +
			"is ready for your own modules, plus a container image, a Makefile, " +
			"and an agent pack an AI coding agent can follow.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := newAppData(args[0], modulePath)
			if err != nil {
				return err
			}
			dir := data.Name
			if !force {
				if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
					return fmt.Errorf("%s/ already exists and is not empty (use --force)", dir)
				}
			}
			written, err := generateApp(dir, data, force)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Scaffolded %s/ (%d files)\n", dir, len(written))
			if !skipTidy {
				if err := runGoModTidy(dir); err != nil {
					fmt.Fprintf(out, "note: resolve dependencies yourself — %v\n", err)
				}
			}
			fmt.Fprintf(out, "\nNext:\n  cd %s\n  go run .            # boots on http://127.0.0.1:8080/admin\n  platformkit new module <name>   # add a tenant-scoped module\n", dir)
			return nil
		},
	}
	cmd.Flags().StringVar(&modulePath, "module", "", "go module path (default: the app name)")
	cmd.Flags().BoolVar(&force, "force", false, "write into a non-empty directory / overwrite files")
	cmd.Flags().BoolVar(&skipTidy, "skip-tidy", false, "do not run `go mod tidy` after writing")
	return cmd
}

func newNewModuleCmd() *cobra.Command {
	var dir string
	var force bool
	cmd := &cobra.Command{
		Use:   "module <name>",
		Short: "Scaffold a tenant-scoped module in the current app",
		Long: "Write one tenant-scoped module — contract, store, migration, HTTP " +
			"surface, and a test — that registers itself with the app. Run from " +
			"an app directory (or pass --dir).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkg := moduleGoPath(dir)
			if pkg == "" {
				return fmt.Errorf("no go.mod in %q — run this inside an app created by `platformkit new app`, or pass --dir", dir)
			}
			data, err := newModuleData(pkg, args[0])
			if err != nil {
				return err
			}
			written, err := generateModule(dir, data, force)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Scaffolded the %s module (%d files):\n", data.Singular, len(written))
			for _, w := range written {
				fmt.Fprintf(out, "  %s\n", w)
			}
			fmt.Fprintf(out, "\nThe module registers itself; just build and run. Its API keys need the %s:read / %s:write scopes.\n", data.Scope, data.Scope)
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", ".", "app directory to write the module into")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing module files")
	return cmd
}

// templateNames is exported for the test that renders every template.
func templateNames() ([]string, error) {
	var out []string
	err := fs.WalkDir(scaffoldTemplates, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			out = append(out, path)
		}
		return nil
	})
	return out, err
}
