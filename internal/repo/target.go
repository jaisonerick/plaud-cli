package repo

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Recording is what a repository is allowed to name a file after.
type Recording struct {
	ID    string
	Name  string
	Start int64 // milliseconds since the epoch
}

// DefaultName is what a transcript is called where the repository says
// nothing: the day it was recorded, what it was called, and enough of the id
// to keep two recordings of one day apart.
const DefaultName = "{date}-{slug}-{short_id}"

var (
	field   = regexp.MustCompile(`\{([a-z_]+)\}`)
	notWord = regexp.MustCompile(`[^\p{L}\p{N}]+`)
)

// Target is the file a recording's transcript belongs in, and the directory
// holding it. The directory is the profile's, then the repository's, then
// whatever the caller passed; the name is a template over the recording.
func (c *Config) Target(p Profile, r Recording, ext string) (string, error) {
	dir := first(p.Dest, c.Dest)
	base := c.Scratch
	if base == "" && c.KeepsCatalog() {
		base = filepath.Join(c.Hub, "transcripts")
	}
	if base == "" {
		base = c.Root
	}

	rendered, err := c.render(dir, r)
	if err != nil {
		return "", err
	}
	if rendered != "" {
		base = c.Abs(rendered)
	}

	name, err := c.render(first(p.Name, c.Name, DefaultName), r)
	if err != nil {
		return "", err
	}
	if filepath.Ext(name) == "" {
		name += ext
	}
	return filepath.Join(base, name), nil
}

// render fills a template with what is known about the recording, and refuses
// a field nothing here can answer rather than writing it into the filename.
func (c *Config) render(template string, r Recording) (string, error) {
	if template == "" {
		return "", nil
	}
	at := c.LocalTime(r.Start)
	known := map[string]string{
		"date":     at.Format("2006-01-02"),
		"year":     at.Format("2006"),
		"month":    at.Format("01"),
		"day":      at.Format("02"),
		"time":     at.Format("15-04"),
		"slug":     slug(r.Name),
		"name":     slug(r.Name),
		"id":       r.ID,
		"short_id": short(r.ID),
	}

	var unknown []string
	out := field.ReplaceAllStringFunc(template, func(match string) string {
		key := match[1 : len(match)-1]
		value, ok := known[key]
		if !ok {
			unknown = append(unknown, key)
			return match
		}
		return value
	})
	if len(unknown) > 0 {
		return "", fmt.Errorf("%q names %s, which is not something known about a recording", template, strings.Join(unknown, " and "))
	}
	return out, nil
}

// LocalTime reads a recording's timestamp in the repository's timezone. A
// repository shared between machines in different zones would otherwise file
// the same recording under two different days.
func (c *Config) LocalTime(ms int64) time.Time {
	at := time.Unix(0, ms*int64(time.Millisecond))
	if c.UTCOffset == nil {
		return at.Local()
	}
	return at.In(time.FixedZone("", *c.UTCOffset*3600))
}

// slug is a recording's title as a filename: letters and digits, joined by
// dashes, short enough that a path built from it stays openable.
func slug(name string) string {
	s := strings.Trim(notWord.ReplaceAllString(name, "-"), "-")
	s = strings.ToLower(s)
	if len(s) > 60 {
		s = strings.Trim(s[:60], "-")
	}
	if s == "" {
		return "recording"
	}
	return s
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
