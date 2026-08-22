// Package repo reads what a repository declares about the transcripts it
// takes in: where they land, what they are called, what describes them, and
// what is kept about them.
//
// Everything here is the repository's decision rather than the caller's, which
// is why it is read from a file at its root instead of passed on every call.
package repo

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

// FileName is what a repository declares itself in.
const FileName = ".plaud.json"

// Config is one repository, as its .plaud.json declares it. A repository that
// declares nothing still produces one of these: the root is then wherever the
// command ran, and File is empty.
type Config struct {
	Root string
	File string

	Context       string
	Filing        string
	Scratch       string
	Hub           string
	Language      string
	Dest          string
	Name          string
	FrontMatter   map[string]string
	ExcludeTags   []string
	ExcludeReason string
	UTCOffset     *int
	Profiles      map[string]Profile

	// Unknown names the keys the file carries that nothing here reads, so a
	// misspelt key is reported rather than silently ignored.
	Unknown []string
}

// Profile is a set of recordings this repository takes in the same way: the
// tag that selects them, and whatever it overrides about where they land.
type Profile struct {
	Tag         string            `json:"tag"`
	Dest        string            `json:"dest"`
	Name        string            `json:"name"`
	Language    string            `json:"language"`
	Context     string            `json:"context"`
	FrontMatter map[string]string `json:"front_matter"`
}

// declared mirrors the file. It is separate from Config so that reading it
// rejects an unknown key, and so that a path becomes absolute exactly once.
type declared struct {
	Context       string             `json:"context"`
	Filing        string             `json:"filing"`
	Scratch       string             `json:"scratch"`
	Hub           string             `json:"hub"`
	Language      string             `json:"language"`
	Dest          string             `json:"dest"`
	Name          string             `json:"name"`
	FrontMatter   map[string]string  `json:"front_matter"`
	ExcludeTags   []string           `json:"exclude_tags"`
	ExcludeReason string             `json:"exclude_reason"`
	UTCOffset     *int               `json:"utc_offset"`
	Profiles      map[string]Profile `json:"profiles"`
}

// Find reads the configuration governing a directory.
//
// The search goes up from the directory itself, and the file's own directory
// is the root. A git root would be the obvious thing to ask instead, and it is
// wrong twice: a command run in a subdirectory of a checkout that holds its own
// declaration would be governed by the outer one, and a directory that is not a
// checkout at all has no root to offer.
func Find(dir string) (*Config, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	for at := dir; ; {
		file := filepath.Join(at, FileName)
		if _, err := os.Stat(file); err == nil {
			return read(file, at)
		}
		parent := filepath.Dir(at)
		if parent == at {
			break
		}
		at = parent
	}
	return &Config{Root: gitRoot(dir, dir)}, nil
}

func read(file, root string) (*Config, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file, err)
	}

	var d declared
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", file, err)
	}

	c := &Config{
		Root:          root,
		File:          file,
		Filing:        d.Filing,
		Language:      d.Language,
		Dest:          d.Dest,
		Name:          d.Name,
		FrontMatter:   d.FrontMatter,
		ExcludeTags:   d.ExcludeTags,
		ExcludeReason: d.ExcludeReason,
		UTCOffset:     d.UTCOffset,
		Profiles:      d.Profiles,
		Unknown:       unknownKeys(data, d),
	}
	c.Context = c.Abs(d.Context)
	c.Scratch = c.Abs(d.Scratch)
	c.Hub = c.Abs(d.Hub)
	if c.ExcludeReason == "" {
		c.ExcludeReason = "excluded-by-config"
	}
	return c, nil
}

// unknownKeys names what the file carries and nothing reads. A key nobody
// reads and nobody reports is a repository configured in a way that quietly
// does not apply.
func unknownKeys(data []byte, d declared) []string {
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	known := map[string]bool{}
	for _, name := range jsonNames(d) {
		known[name] = true
	}
	var unknown []string
	for key := range raw {
		if !known[key] {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// Abs resolves a path the file declares, which is relative to the root.
func (c *Config) Abs(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(c.Root, path)
}

// Rel writes a path the way this repository refers to it, and leaves anything
// outside it alone.
func (c *Config) Rel(path string) string {
	if path == "" || c.Root == "" {
		return path
	}
	rel, err := filepath.Rel(c.Root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// Declares reports whether a repository said anything at all.
func (c *Config) Declares() bool { return c.File != "" }

// KeepsCatalog reports whether this repository keeps track of the recordings
// it knows about, rather than fetching them one at a time.
func (c *Config) KeepsCatalog() bool { return c.Hub != "" }

// Profile returns a named profile, and reports whether it is declared.
func (c *Config) Profile(name string) (Profile, bool) {
	p, ok := c.Profiles[name]
	return p, ok
}

// ProfileNames lists the profiles in the order a person would read them.
func (c *Config) ProfileNames() []string {
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// gitRoot is the fallback for a directory that declares nothing, so that a
// command run deep inside a checkout still writes against its top.
func gitRoot(dir, fallback string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return fallback
	}
	if root := strings.TrimSpace(string(out)); root != "" {
		return root
	}
	return fallback
}

// jsonNames is every key the file is read for, taken from the struct that
// reads it so the two cannot drift apart.
func jsonNames(v any) []string {
	t := reflect.TypeOf(v)
	names := make([]string, 0, t.NumField())
	for i := range t.NumField() {
		tag, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if tag != "" && tag != "-" {
			names = append(names, tag)
		}
	}
	return names
}
