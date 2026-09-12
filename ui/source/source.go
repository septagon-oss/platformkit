// Package source verifies and persists bounded edits to captured Go properties.
// It is development tooling: producers are trusted programs, not sandboxed input.
package source

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/ui"
)

// Target locates the start of an existing capture call in an owning module.
// Column zero admits a unique call on Line. SHA256 addresses the entire file,
// independently of the proposal's semantic export hash.
type Target struct {
	File         string
	Line, Column int
	SHA256       string
}

// GoProducer selects a caller-owned main package in Dir, its module root.
// Package is . or a relative ./path. Args select the full source composition.
// The producer must emit one DesignExport JSON object, and accept the same Args
// followed by --proposal with PropsProposal JSON on stdin. Builds disable cgo,
// workspaces and GOFLAGS; custom build configurations are not supported yet.
// Constructors must be deterministic and free of external side effects.
type GoProducer struct {
	Dir, Package string
	Args         []string
}

// Review is detached evidence, not authority to apply arbitrary source bytes.
type Review struct {
	File         string `json:"file"`
	BeforeSHA256 string `json:"beforeSHA256"`
	AfterSHA256  string `json:"afterSHA256"`
	BaseSHA256   string `json:"baseSHA256"`
	ResultSHA256 string `json:"resultSHA256"`
	Source       string `json:"source"`
}

// Change retains the verified candidate and its input revisions privately.
type Change struct {
	producer GoProducer
	env      []string
	target   Target
	inputs   map[string]string
	review   Review
	mode     os.FileMode
}

// Review returns the complete candidate file and source/export revisions.
func (c *Change) Review() Review { return c.review }

// Prepare runs the actual producer, its existing proposal operation and a fresh
// build with the candidate Go overlay. All three must agree on the full export.
// Only existing keyed string literals can change; no source files are written.
// Temporary overlays and module copies are removed before returning.
func Prepare(ctx context.Context, producer GoProducer, target Target, proposal ui.PropsProposal) (*Change, error) {
	root, err := filepath.Abs(producer.Dir)
	if err != nil {
		return nil, err
	}
	producer.Dir, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if producer.Package != "." && (!strings.HasPrefix(producer.Package, "./") || strings.Contains(producer.Package, "...")) {
		return nil, fmt.Errorf("source: producer must name one local main package")
	}
	if _, err := ownedPath(producer.Dir, producer.Package); err != nil {
		return nil, err
	}
	filename, err := ownedPath(producer.Dir, target.File)
	if err != nil {
		return nil, err
	}
	target.File = filename
	before, mode, err := readTarget(target)
	if err != nil {
		return nil, err
	}
	producer.Args = slices.Clone(producer.Args)
	env := append(os.Environ(), "GOWORK=off", "GOFLAGS=", "CGO_ENABLED=0")
	session, err := newSession(producer, env)
	if err != nil {
		return nil, err
	}
	defer session.close()
	inputs, err := session.inputs(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := inputs[filename]; !ok {
		return nil, fmt.Errorf("source: target is not a producer build input")
	}
	if inputs[filename] != revision(mode, before) {
		return nil, fmt.Errorf("source: source bytes or permissions changed during preparation")
	}
	baseline, err := session.snapshot(ctx, "", nil)
	if err != nil {
		return nil, err
	}
	if baseline.SHA256 != proposal.BaseSHA256 {
		return nil, ui.ErrStaleExport
	}
	body, err := json.Marshal(proposal)
	if err != nil {
		return nil, err
	}
	expected, err := session.snapshot(ctx, "", body)
	if err != nil {
		return nil, err
	}
	after, err := rewrite(ctx, producer.Dir, filename, before, target.Line, target.Column, proposal.Props, "-modfile="+session.modfile)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(before, after) || expected.SHA256 == baseline.SHA256 {
		return nil, fmt.Errorf("source: proposal must change the captured export")
	}
	overlay, err := session.overlay(filename, after)
	if err != nil {
		return nil, err
	}
	rebuilt, err := session.snapshot(ctx, overlay, nil)
	if err != nil {
		return nil, err
	}
	want, _ := json.Marshal(expected)
	got, _ := json.Marshal(rebuilt)
	if !bytes.Equal(want, got) {
		return nil, fmt.Errorf("source: rebuilt export differs from the proposed export; check the source location")
	}
	if err := session.unchanged(ctx, inputs); err != nil {
		return nil, err
	}
	if _, _, err := readTarget(target); err != nil {
		return nil, err
	}
	return &Change{
		producer: producer, env: env, target: target, inputs: inputs, mode: mode,
		review: Review{filename, digest(before), digest(after), baseline.SHA256, rebuilt.SHA256, string(after)},
	}, nil
}

// Apply rechecks the producer's build inputs and replaces exactly one source
// file using a same-directory atomic rename. Unix advisory locking serializes
// cooperating writers; other platforms refuse. An unrelated editor can still
// race the last check and rename, so this is not filesystem compare-and-swap.
// File mode is preserved; ownership, ACLs and extended attributes are not copied.
func (c *Change) Apply(ctx context.Context) error {
	if c == nil || c.inputs == nil {
		return fmt.Errorf("source: no verified change")
	}
	unlock, err := lock(c.producer.Dir)
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := ownedPath(c.producer.Dir, c.target.File); err != nil {
		return err
	}
	session, err := newSession(c.producer, c.env)
	if err != nil {
		return err
	}
	defer session.close()
	if err := session.unchanged(ctx, c.inputs); err != nil {
		return err
	}
	staged, err := os.CreateTemp(filepath.Dir(c.target.File), ".pkit-source-*")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()
	if err := staged.Chmod(c.mode.Perm()); err != nil {
		return err
	}
	if _, err := staged.WriteString(c.review.Source); err != nil {
		return err
	}
	if err := staged.Sync(); err != nil {
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	// Re-read every known input immediately before the rename. Discovery above
	// also catches newly added build files; neither check locks external editors.
	if err := checkFiles(c.inputs); err != nil {
		return err
	}
	if _, err := ownedPath(c.producer.Dir, c.target.File); err != nil {
		return err
	}
	if _, _, err := readTarget(c.target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(staged.Name(), c.target.File)
}

func ownedPath(root, name string) (string, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	name = filepath.Clean(name)
	rel, err := filepath.Rel(root, name)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source: path is outside the owning module")
	}
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", err
	}
	if resolved != name {
		return "", fmt.Errorf("source: symlink paths are not writable source targets")
	}
	return name, nil
}

func readTarget(target Target) ([]byte, os.FileMode, error) {
	info, err := os.Lstat(target.File)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || filepath.Ext(target.File) != ".go" {
		return nil, 0, fmt.Errorf("source: target must be a regular Go source file")
	}
	body, err := os.ReadFile(target.File)
	if err != nil {
		return nil, 0, err
	}
	if digest(body) != target.SHA256 {
		return nil, 0, fmt.Errorf("source: source file revision is stale")
	}
	return body, info.Mode(), nil
}

func digest(body []byte) string { return fmt.Sprintf("%x", sha256.Sum256(body)) }

func (s *session) unchanged(ctx context.Context, before map[string]string) error {
	after, err := s.inputs(ctx)
	if err != nil {
		return err
	}
	if !maps.Equal(before, after) {
		return fmt.Errorf("source: producer build inputs changed; prepare again")
	}
	return nil
}
