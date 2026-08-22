// Package catalog keeps track of the recordings a repository knows about:
// what each one is, whether it has been transcribed, and where it was filed.
//
// It is a JSON-lines file at the repository, one object per recording, and it
// is the whole of what is stored. Half of each entry is what Plaud says and is
// replaced on every refresh; the other half is what a person decided, and a
// refresh never touches it.
package catalog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileName is the catalog itself, git-tracked so the curation in it is
// versioned with the repository it describes.
const FileName = "catalog.jsonl"

// Entry is one recording. The fields Plaud answers for are values; the ones a
// person fills in are pointers, so a field nobody has decided reads as null
// rather than as an empty string somebody typed.
type Entry struct {
	ID          string   `json:"id"`
	Filename    string   `json:"filename"`
	StartTime   int64    `json:"start_time"`
	RecordedAt  string   `json:"recorded_at"`
	DurationMS  int64    `json:"duration_ms"`
	DurationMin float64  `json:"duration_min"`
	Scene       int      `json:"scene"`
	URL         string   `json:"url"`
	IsTrans     bool     `json:"is_trans"`
	IsSummary   bool     `json:"is_summary"`
	Tags        []string `json:"tags"`

	Status         string  `json:"status"`
	ExcludedReason *string `json:"excluded_reason"`
	Project        *string `json:"project"`
	Path           *string `json:"path"`
	Repo           *string `json:"repo"`
	TranscriptPath *string `json:"transcript_path"`
	SummaryPath    *string `json:"summary_path"`
	Notes          *string `json:"notes"`
}

// Statuses a recording moves through. The first two are recomputed on every
// refresh; the last two are a person's decision and are never overwritten.
const (
	Pending     = "pending"
	Transcribed = "transcribed"
	Filed       = "filed"
	Excluded    = "excluded"
)

// Recomputed reports whether a refresh may still decide this entry's status. A
// recording somebody filed or ruled out has been decided, and deciding it
// again from a listing would undo them.
func (e Entry) Recomputed() bool {
	switch e.Status {
	case "", Pending, Transcribed:
		return true
	}
	return false
}

// Catalog is the file, in the order it will be written back.
type Catalog struct {
	path    string
	entries map[string]*Entry
}

// Open reads the catalog of a hub, and returns an empty one where no file
// exists yet.
func Open(hub string) (*Catalog, error) {
	c := &Catalog{path: filepath.Join(hub, FileName), entries: map[string]*Entry{}}

	file, err := os.Open(c.path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for line := 1; scan.Scan(); line++ {
		text := strings.TrimSpace(scan.Text())
		if text == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(text), &e); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", c.path, line, err)
		}
		c.entries[e.ID] = &e
	}
	return c, scan.Err()
}

// Path is the file this catalog is read from and written to.
func (c *Catalog) Path() string { return c.path }

// Get returns the entry for a recording, and whether the catalog knows it.
func (c *Catalog) Get(id string) (*Entry, bool) {
	e, ok := c.entries[id]
	return e, ok
}

// Put adds or replaces an entry.
func (c *Catalog) Put(e *Entry) { c.entries[e.ID] = e }

// Entries lists the catalog newest first, which is the order it is written in
// and the order somebody reading it wants.
func (c *Catalog) Entries() []*Entry {
	all := make([]*Entry, 0, len(c.entries))
	for _, e := range c.entries {
		all = append(all, e)
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].StartTime > all[j].StartTime })
	return all
}

// Len is how many recordings the catalog holds.
func (c *Catalog) Len() int { return len(c.entries) }

// Save writes the catalog back, atomically, so a run interrupted halfway does
// not leave a repository with half its curation.
func (c *Catalog) Save() error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// The file is read by people and by diffs, and Go escapes <, > and & into
	// \u sequences unless told otherwise, which rewrites lines nothing changed.
	enc.SetEscapeHTML(false)
	for _, e := range c.Entries() {
		// A recording carrying no tag has an empty list, not a missing one:
		// nothing distinguishes the two, and writing null would rewrite every
		// line the file already has.
		if e.Tags == nil {
			e.Tags = []string{}
		}
		if err := enc.Encode(e); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(c.path), 0755); err != nil {
		return err
	}
	temp := c.path + ".writing"
	if err := os.WriteFile(temp, buf.Bytes(), 0644); err != nil {
		return err
	}
	return os.Rename(temp, c.path)
}

// Text is a helper for the fields a person fills in, which are absent rather
// than empty when nobody has decided them.
func Text(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Read is the other direction, for printing a field that may be absent.
func Read(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
