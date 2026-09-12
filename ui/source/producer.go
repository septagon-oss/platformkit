package source

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/septagon-oss/platformkit/ui"
)

type session struct {
	producer GoProducer
	env      []string
	dir      string
	modfile  string
	modules  map[string]string
}

func newSession(producer GoProducer, env []string) (*session, error) {
	dir, err := os.MkdirTemp("", "pkit-source-")
	if err != nil {
		return nil, err
	}
	s := &session{producer: producer, env: env, dir: dir, modfile: filepath.Join(dir, "go.mod"), modules: map[string]string{}}
	// A separate modfile also redirects go.sum writes that -mod=readonly alone
	// does not prevent. Relative replace paths still use the real module root.
	for _, name := range []string{"go.mod", "go.sum"} {
		original := filepath.Join(producer.Dir, name)
		info, err := os.Stat(original)
		if errors.Is(err, os.ErrNotExist) && name == "go.sum" {
			s.modules[original] = "missing"
			continue
		}
		if err != nil {
			s.close()
			return nil, err
		}
		body, err := os.ReadFile(original)
		if err == nil {
			s.modules[original] = revision(info.Mode(), body)
			err = os.WriteFile(filepath.Join(dir, name), body, 0600)
		}
		if err != nil {
			s.close()
			return nil, err
		}
	}
	return s, nil
}

func (s *session) close() { os.RemoveAll(s.dir) }

func (s *session) command(ctx context.Context, operation string, flags, args []string, input []byte) ([]byte, error) {
	argv := []string{operation, "-mod=readonly", "-modfile=" + s.modfile}
	argv = append(argv, flags...)
	argv = append(argv, s.producer.Package)
	argv = append(argv, args...)
	cmd := exec.CommandContext(ctx, "go", argv...)
	cmd.Dir, cmd.Env, cmd.Stdin = s.producer.Dir, s.env, bytes.NewReader(input)
	cmd.WaitDelay = 5 * time.Second
	stdout, stderr := &boundedBuffer{limit: 32 << 20}, &boundedBuffer{limit: 64 << 10}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("source: go %s failed: %w\n%s", operation, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, fmt.Errorf("producer output exceeds %d bytes", b.limit)
	}
	return b.Buffer.Write(p)
}

func (s *session) snapshot(ctx context.Context, overlay string, proposal []byte) (ui.DesignExport, error) {
	var flags []string
	if overlay != "" {
		flags = append(flags, "-overlay="+overlay)
	}
	args := append([]string{}, s.producer.Args...)
	if proposal != nil {
		args = append(args, "--proposal")
	}
	body, err := s.command(ctx, "run", flags, args, proposal)
	if err != nil {
		return ui.DesignExport{}, err
	}
	var snapshot ui.DesignExport
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&snapshot); err != nil {
		return snapshot, fmt.Errorf("source: decode producer snapshot: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return snapshot, fmt.Errorf("source: producer must emit exactly one snapshot")
	}
	hash := snapshot.SHA256
	snapshot.SHA256 = ""
	canonical, err := json.Marshal(snapshot)
	snapshot.SHA256 = hash
	if err != nil || hash != digest(canonical) {
		return snapshot, fmt.Errorf("source: producer snapshot has an invalid content hash")
	}
	if snapshot.Schema != "platformkit.design-export.v1" && snapshot.Schema != "platformkit.design-export.v2" {
		return snapshot, fmt.Errorf("source: unsupported producer snapshot schema")
	}
	return snapshot, nil
}

func (s *session) overlay(filename string, source []byte) (string, error) {
	replacement := filepath.Join(s.dir, "candidate.go")
	if err := os.WriteFile(replacement, source, 0600); err != nil {
		return "", err
	}
	body, err := json.Marshal(struct{ Replace map[string]string }{map[string]string{filename: replacement}})
	if err != nil {
		return "", err
	}
	filename = filepath.Join(s.dir, "overlay.json")
	return filename, os.WriteFile(filename, body, 0600)
}

// inputs rediscoveries catch new build files as well as changed existing files.
// Runtime files, network data and program side effects are outside this contract.
func (s *session) inputs(ctx context.Context) (map[string]string, error) {
	if err := checkFiles(s.modules); err != nil {
		return nil, err
	}
	body, err := s.command(ctx, "list", []string{"-deps", "-json"}, nil, nil)
	if err != nil {
		return nil, err
	}
	files := map[string]string{}
	decoder := json.NewDecoder(bytes.NewReader(body))
	for {
		var pkg struct {
			Dir                                            string
			GoFiles, HFiles, SFiles, SysoFiles, EmbedFiles []string
			Module                                         *struct {
				GoMod string
				Main  bool
			}
		}
		if err := decoder.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		for _, names := range [][]string{pkg.GoFiles, pkg.HFiles, pkg.SFiles, pkg.SysoFiles, pkg.EmbedFiles} {
			for _, name := range names {
				files[filepath.Join(pkg.Dir, name)] = ""
			}
		}
		if pkg.Module != nil && !pkg.Module.Main && pkg.Module.GoMod != "" {
			files[pkg.Module.GoMod] = ""
		}
	}
	for filename := range s.modules {
		files[filename] = ""
	}
	for filename := range files {
		files[filename], err = fileRevision(filename)
		if err != nil {
			return nil, err
		}
	}
	return files, checkFiles(s.modules)
}

func fileRevision(filename string) (string, error) {
	info, err := os.Stat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return "missing", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("source: build input is not a regular file: %s", filename)
	}
	body, err := os.ReadFile(filename)
	return revision(info.Mode(), body), err
}

func revision(mode os.FileMode, body []byte) string {
	return fmt.Sprintf("%s:%s", mode, digest(body))
}

func checkFiles(files map[string]string) error {
	for filename, expected := range files {
		actual, err := fileRevision(filename)
		if err != nil {
			return err
		}
		if actual != expected {
			return fmt.Errorf("source: build input changed: %s", filename)
		}
	}
	return nil
}
