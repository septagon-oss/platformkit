package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

type repository struct{ root string }
type source struct {
	path string
	data []byte
}

func (r repository) git(input []byte, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", r.root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1")
	cmd.Stdin = bytes.NewReader(input)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %q: %w: %s", args, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

func openRepository(root string) (repository, error) {
	r := repository{root}
	out, err := r.git(nil, "rev-parse", "--show-toplevel")
	if err == nil {
		r.root = strings.TrimSuffix(string(out), "\n")
	}
	return r, err
}

func selected(path string) bool {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
		return false
	}
	for part := range strings.SplitSeq(path, "/") {
		if slices.Contains([]string{"vendor", "node_modules", ".git", "testdata"}, part) {
			return false
		}
	}
	return true
}

func (r repository) sources(revision string) ([]source, error) {
	args := []string{"ls-files", "-z", "-c", "-o", "--exclude-standard"}
	if revision != "" {
		args = []string{"ls-tree", "-r", "-z", "--full-tree", revision}
	}
	out, err := r.git(nil, args...)
	if err != nil {
		return nil, err
	}
	paths, modes := []string{}, map[string]string{}
	for entry := range strings.SplitSeq(string(out), "\x00") {
		path, mode := entry, ""
		if revision != "" && entry != "" {
			header, name, ok := strings.Cut(entry, "\t")
			fields := strings.Fields(header)
			if !ok || len(fields) != 3 {
				return nil, fmt.Errorf("invalid tree entry %q", entry)
			}
			path, mode = name, fields[0]
		}
		if selected(path) {
			if revision == "" {
				if _, err := sourcePath(r.root, path); os.IsNotExist(err) {
					continue
				}
			}
			paths = append(paths, path)
			modes[path] = mode
		}
	}
	slices.Sort(paths)
	paths = slices.Compact(paths)
	generated, err := r.generated(paths, revision)
	if err != nil {
		return nil, err
	}
	var files []source
	for _, path := range paths {
		if generated[path] {
			continue
		}
		var data []byte
		if revision == "" {
			data, err = readSource(r.root, path)
			if os.IsNotExist(err) {
				continue
			}
		} else if modes[path] != "100644" && modes[path] != "100755" {
			return nil, fmt.Errorf("%s:%q: selected source is not a regular file", revision, path)
		} else {
			data, err = r.git(nil, "cat-file", "blob", revision+":"+path)
		}
		if err != nil {
			return nil, err
		}
		files = append(files, source{path, data})
	}
	return files, nil
}

func readSource(root, path string) ([]byte, error) {
	name, err := sourcePath(root, path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%q: selected input is not a regular file", path)
	}
	return os.ReadFile(name)
}

func sourcePath(root, path string) (string, error) {
	current := root
	for part := range strings.SplitSeq(path, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("%q: selected symlink", path)
		}
	}
	return current, nil
}

func (r repository) generated(paths []string, revision string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(paths) == 0 {
		return result, nil
	}
	args := []string{"check-attr", "-z", "--stdin"}
	if revision != "" {
		args = append(args, "--source="+revision)
	}
	args = append(args, "linguist-generated")
	out, err := r.git([]byte(strings.Join(paths, "\x00")+"\x00"), args...)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
	if len(parts) != 3*len(paths) {
		return nil, fmt.Errorf("git check-attr: incomplete response")
	}
	for i, path := range paths {
		if parts[3*i] != path || parts[3*i+1] != "linguist-generated" {
			return nil, fmt.Errorf("git check-attr: unexpected response for %q", path)
		}
		switch value := parts[3*i+2]; value {
		case "set", "true":
			result[path] = true
		case "unset", "false", "unspecified":
		default:
			return nil, fmt.Errorf("%q: invalid linguist-generated value %q", path, value)
		}
	}
	return result, nil
}
