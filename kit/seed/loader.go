// Package seed loads an application's embedded starter and demo records. It
// does not discover writers: the application supplies those explicitly.
package seed

import (
	"bytes"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const version = "platformkit.seed/v1"

// Source identifies the input that produced a record or value.
type Source struct {
	File         string
	Line, Column int
}

func (s Source) String() string { return fmt.Sprintf("%s:%d:%d", s.File, s.Line, s.Column) }

// Record is one seed entry. Fields and command arguments remain owner inputs;
// the loader does not validate or silently change business values.
type Record struct {
	Key      string
	Fields   map[string]any
	Commands []Command
	I18n     map[string]map[string]any
	Asset    string
	Source   Source
	Values   map[string]Source // source of each top-level field and command argument
}

// Command names an owning module's write command and its arguments.
type Command struct {
	Name   string
	Args   map[string]any
	Source Source
}

// Document contains one resource's records for one seed kind.
type Document struct {
	Kind, Resource string
	Prune          bool
	Records        []Record
	Source         Source
}

// Load reads the named kinds from an embedded filesystem root. A kind holds at
// most one YAML or JSON file per resource. Its output preserves file and record
// order; a caller may then order records by declared dependencies.
func Load(files fs.FS, root string, kinds ...string) ([]Document, error) {
	if files == nil || !fs.ValidPath(root) {
		return nil, fmt.Errorf("seed: invalid filesystem or root %q", root)
	}
	var documents []Document
	seen := make(map[string]Source)
	for _, kind := range kinds {
		if !validName(kind) {
			return nil, fmt.Errorf("seed: invalid kind %q", kind)
		}
		dir := path.Join(root, kind)
		entries, err := fs.ReadDir(files, dir)
		if err != nil {
			return nil, fmt.Errorf("seed: read %s: %w", dir, err)
		}
		resources := make(map[string]string)
		for _, entry := range entries {
			if entry.IsDir() {
				continue // assets may live beside resource files
			}
			ext := path.Ext(entry.Name())
			if ext != ".yaml" && ext != ".json" {
				continue
			}
			resource := strings.TrimSuffix(entry.Name(), ext)
			if !validName(resource) {
				return nil, fmt.Errorf("seed: invalid resource filename %s", entry.Name())
			}
			if prior := resources[resource]; prior != "" {
				return nil, fmt.Errorf("seed: %s and %s both define %s in %s", path.Join(dir, prior), path.Join(dir, entry.Name()), resource, kind)
			}
			resources[resource] = entry.Name()
			filename := path.Join(dir, entry.Name())
			if len(filename) > 512 || !fs.ValidPath(filename) {
				return nil, fmt.Errorf("seed: invalid source path %q", filename)
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				return nil, fmt.Errorf("seed: %s is not a regular file: %v", filename, err)
			}
			data, err := fs.ReadFile(files, filename)
			if err != nil {
				return nil, fmt.Errorf("seed: read %s: %w", filename, err)
			}
			doc, err := parse(data, filename, kind, resource, files)
			if err != nil {
				return nil, err
			}
			for _, record := range doc.Records {
				identity := resource + "/" + record.Key
				if prior, ok := seen[identity]; ok {
					return nil, fmt.Errorf("seed: %s duplicates %s at %s", record.Source, identity, prior)
				}
				seen[identity] = record.Source
			}
			documents = append(documents, doc)
		}
	}
	return documents, nil
}

func validName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	for _, r := range name {
		if r < 'a' || r > 'z' {
			if r < '0' || r > '9' {
				if r != '-' && r != '_' {
					return false
				}
			}
		}
	}
	return true
}

func validCommandName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func parse(data []byte, filename, kind, resource string, files fs.FS) (Document, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := dec.Decode(&root); err != nil {
		return Document{}, fmt.Errorf("seed: %s: %w", filename, err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		return Document{}, fmt.Errorf("seed: %s: multiple documents or trailing input: %v", filename, err)
	}
	if len(root.Content) != 1 {
		return Document{}, fmt.Errorf("seed: %s: expected one document", filename)
	}
	body := root.Content[0]
	if err := checkNode(body, filename); err != nil {
		return Document{}, err
	}
	fields, err := mapping(body, filename, "apiVersion", "resource", "prune", "records")
	if err != nil {
		return Document{}, err
	}
	if got := scalar(fields["apiVersion"]); got != version {
		return Document{}, fmt.Errorf("seed: %s: unsupported apiVersion %q", source(filename, fields["apiVersion"]), got)
	}
	if got := scalar(fields["resource"]); got != resource {
		return Document{}, fmt.Errorf("seed: %s: resource %q does not match filename %q", filename, got, resource)
	}
	doc := Document{Kind: kind, Resource: resource, Source: source(filename, body)}
	if prune := fields["prune"]; prune != nil {
		if prune.Tag != "!!bool" {
			return Document{}, fmt.Errorf("seed: %s: prune must be a boolean", source(filename, prune))
		}
		doc.Prune = strings.EqualFold(prune.Value, "true")
	}
	items := fields["records"]
	if items == nil || items.Kind != yaml.SequenceNode {
		return Document{}, fmt.Errorf("seed: %s: records must be a sequence", filename)
	}
	for _, item := range items.Content {
		record, err := parseRecord(item, filename, files)
		if err != nil {
			return Document{}, err
		}
		doc.Records = append(doc.Records, record)
	}
	return doc, nil
}

func parseRecord(node *yaml.Node, filename string, files fs.FS) (Record, error) {
	values, err := mapping(node, filename, "key", "fields", "commands", "i18n", "asset")
	if err != nil {
		return Record{}, err
	}
	key := scalar(values["key"])
	if key == "" || len(key) > 1024 || !utf8.ValidString(key) || strings.IndexFunc(key, unicode.IsControl) >= 0 {
		return Record{}, fmt.Errorf("seed: %s: invalid record key", source(filename, node))
	}
	record := Record{Key: key, Source: source(filename, node), Values: make(map[string]Source)}
	if fields := values["fields"]; fields != nil {
		members, err := mapping(fields, filename)
		if err != nil {
			return Record{}, err
		}
		if err := fields.Decode(&record.Fields); err != nil {
			return Record{}, fmt.Errorf("seed: %s: fields: %w", source(filename, fields), err)
		}
		for name, value := range members {
			record.Values["fields/"+name] = source(filename, value)
		}
	}
	if commands := values["commands"]; commands != nil {
		if commands.Kind != yaml.SequenceNode {
			return Record{}, fmt.Errorf("seed: %s: commands must be a sequence", source(filename, commands))
		}
		for _, item := range commands.Content {
			members, err := mapping(item, filename)
			if err != nil {
				return Record{}, err
			}
			name := scalar(members["name"])
			if !validCommandName(name) {
				return Record{}, fmt.Errorf("seed: %s: command needs a name", source(filename, item))
			}
			args := make(map[string]any)
			for field, value := range members {
				if field == "name" {
					continue
				}
				var decoded any
				if err := value.Decode(&decoded); err != nil {
					return Record{}, fmt.Errorf("seed: %s: command %s: %w", source(filename, value), field, err)
				}
				args[field] = decoded
				record.Values["commands/"+name+"/"+field] = source(filename, value)
			}
			record.Commands = append(record.Commands, Command{Name: name, Args: args, Source: source(filename, item)})
		}
	}
	if translations := values["i18n"]; translations != nil {
		if _, err := mapping(translations, filename); err != nil {
			return Record{}, err
		}
		if err := translations.Decode(&record.I18n); err != nil {
			return Record{}, fmt.Errorf("seed: %s: i18n: %w", source(filename, translations), err)
		}
	}
	if asset := values["asset"]; asset != nil {
		name := scalar(asset)
		if name == "" || strings.HasPrefix(name, "/") || slices.Contains(strings.Split(name, "/"), "..") || !fs.ValidPath(name) {
			return Record{}, fmt.Errorf("seed: %s: unsafe asset path %q", source(filename, asset), name)
		}
		record.Asset = path.Join(path.Dir(filename), name)
		info, err := fs.Stat(files, record.Asset)
		if err != nil || !info.Mode().IsRegular() {
			return Record{}, fmt.Errorf("seed: %s: asset %q is not a regular file: %v", source(filename, asset), name, err)
		}
	}
	return record, nil
}

func source(file string, node *yaml.Node) Source {
	if node == nil {
		return Source{File: file}
	}
	return Source{File: file, Line: node.Line, Column: node.Column}
}

func scalar(node *yaml.Node) string {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return ""
	}
	return node.Value
}

func mapping(node *yaml.Node, file string, allowed ...string) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("seed: %s: expected a mapping", source(file, node))
	}
	out := make(map[string]*yaml.Node, len(node.Content)/2)
	for i := 0; i < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
			return nil, fmt.Errorf("seed: %s: mapping keys must be strings", source(file, key))
		}
		if _, exists := out[key.Value]; exists {
			return nil, fmt.Errorf("seed: %s: duplicate key %q", source(file, key), key.Value)
		}
		if strings.Contains(strings.ToLower(key.Value), "password") {
			return nil, fmt.Errorf("seed: %s: passwords must come from the application, not a seed file", source(file, key))
		}
		if allowed != nil && !slices.Contains(allowed, key.Value) {
			return nil, fmt.Errorf("seed: %s: unknown field %q", source(file, key), key.Value)
		}
		out[key.Value] = value
	}
	return out, nil
}

func checkNode(node *yaml.Node, file string) error {
	if node.Kind == yaml.AliasNode {
		return fmt.Errorf("seed: %s: YAML aliases are unsupported", source(file, node))
	}
	if node.Kind == yaml.MappingNode {
		if _, err := mapping(node, file); err != nil {
			return err
		}
	}
	for _, child := range node.Content {
		if err := checkNode(child, file); err != nil {
			return err
		}
	}
	return nil
}
