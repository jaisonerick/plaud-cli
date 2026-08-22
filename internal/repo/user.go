package repo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// SettingsFile holds what is true of the person running the command rather
// than of the repository, and is never committed.
//
// A profile has two halves with different owners. Where transcripts of a kind
// go, what they are called and what their front matter carries is the
// repository's, and everyone working in it agrees. Which recordings are of
// that kind is the account's: a tag lives in one person's Plaud, and committing
// it hands everyone else a profile that selects nothing.
const SettingsFile = "settings.json"

// Settings is that file. `defaults` applies wherever the person works;
// `repositories` is keyed by what identifies a repository across checkouts.
type Settings struct {
	Defaults     *Layer            `json:"defaults,omitempty"`
	Repositories map[string]*Layer `json:"repositories,omitempty"`

	path string
}

// Layer is anything a repository declares, said by a person instead. Every key
// is optional: what is set wins over the layer beneath it, and what is not is
// not an instruction to clear anything.
type Layer struct {
	Context       string             `json:"context,omitempty"`
	Filing        string             `json:"filing,omitempty"`
	Scratch       string             `json:"scratch,omitempty"`
	Hub           string             `json:"hub,omitempty"`
	Language      string             `json:"language,omitempty"`
	Dest          string             `json:"dest,omitempty"`
	Name          string             `json:"name,omitempty"`
	FrontMatter   map[string]string  `json:"front_matter,omitempty"`
	ExcludeTags   []string           `json:"exclude_tags,omitempty"`
	ExcludeReason string             `json:"exclude_reason,omitempty"`
	UTCOffset     *int               `json:"utc_offset,omitempty"`
	Profiles      map[string]Profile `json:"profiles,omitempty"`
}

var remoteURL = regexp.MustCompile(`^(?:[\w.+-]+@|[a-z]+://(?:[\w.+-]+@)?)?([^/:]+)[:/](.+?)(?:\.git)?/?$`)

// LoadSettings reads the person's own settings, and returns an empty set where
// there is no file, so a machine that has never been configured still runs.
func LoadSettings() (*Settings, error) {
	path, err := settingsPath()
	if err != nil {
		return nil, err
	}

	s := &Settings{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	s.path = path
	return s, nil
}

// Path is the file these settings are read from and written to.
func (s *Settings) Path() string { return s.path }

// For returns the layer this person set for one repository, creating it when
// asked to, so a caller writing a setting does not have to build the map.
func (s *Settings) For(identity string, create bool) *Layer {
	if layer := s.Repositories[identity]; layer != nil {
		return layer
	}
	if !create {
		return nil
	}
	if s.Repositories == nil {
		s.Repositories = map[string]*Layer{}
	}
	s.Repositories[identity] = &Layer{}
	return s.Repositories[identity]
}

// Save writes the settings back, readable only by their owner: the file says
// which of somebody's recordings feed which piece of work.
func (s *Settings) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	return os.WriteFile(s.path, append(data, '\n'), 0600)
}

// Identity is what names a repository in a person's settings: where it is
// hosted, which survives a fresh clone, a second checkout and another machine.
// A repository with no remote is named by its path, which is all it has.
func Identity(root string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return root
	}
	found := remoteURL.FindStringSubmatch(strings.TrimSpace(string(out)))
	if found == nil {
		return root
	}
	return found[1] + "/" + found[2]
}

// apply lays one layer over the configuration, key by key. A key the layer
// does not set leaves what is underneath standing, which is what makes a
// person's settings an addition rather than a replacement.
func (c *Config) apply(l *Layer, from string) {
	if l == nil {
		return
	}
	set := func(target *string, value string, key string) {
		if value != "" {
			*target = value
			c.from[key] = from
		}
	}
	set(&c.Filing, l.Filing, "filing")
	set(&c.Language, l.Language, "language")
	set(&c.Dest, l.Dest, "dest")
	set(&c.Name, l.Name, "name")
	set(&c.ExcludeReason, l.ExcludeReason, "exclude_reason")
	if l.Context != "" {
		c.Context, c.from["context"] = c.Abs(l.Context), from
	}
	if l.Scratch != "" {
		c.Scratch, c.from["scratch"] = c.Abs(l.Scratch), from
	}
	if l.Hub != "" {
		c.Hub, c.from["hub"] = c.Abs(l.Hub), from
	}
	if l.UTCOffset != nil {
		c.UTCOffset, c.from["utc_offset"] = l.UTCOffset, from
	}
	if len(l.ExcludeTags) > 0 {
		c.ExcludeTags, c.from["exclude_tags"] = l.ExcludeTags, from
	}
	for key, value := range l.FrontMatter {
		if c.FrontMatter == nil {
			c.FrontMatter = map[string]string{}
		}
		c.FrontMatter[key] = value
		c.from["front_matter"] = from
	}
	for name, profile := range l.Profiles {
		c.mergeProfile(name, profile, from)
	}
}

// mergeProfile lays a person's half of a profile over the repository's. The
// two halves are usually disjoint — the repository says where, the person says
// which recordings — so replacing the whole profile would drop one of them.
func (c *Config) mergeProfile(name string, over Profile, from string) {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	under := c.Profiles[name]

	if over.Tag != "" {
		under.Tag = over.Tag
		c.from["profiles."+name+".tag"] = from
	}
	if over.Dest != "" {
		under.Dest = over.Dest
	}
	if over.Name != "" {
		under.Name = over.Name
	}
	if over.Language != "" {
		under.Language = over.Language
	}
	if over.Context != "" {
		under.Context = over.Context
	}
	for key, value := range over.FrontMatter {
		if under.FrontMatter == nil {
			under.FrontMatter = map[string]string{}
		}
		under.FrontMatter[key] = value
	}
	c.Profiles[name] = under
}

func settingsPath() (string, error) {
	if named := os.Getenv("PLAUD_SETTINGS"); named != "" {
		return named, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "plaud", SettingsFile), nil
}
