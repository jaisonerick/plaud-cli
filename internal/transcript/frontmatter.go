package transcript

import (
	"regexp"
	"sort"
	"strings"
)

// Field is one scalar entry of a transcript's front matter.
type Field struct{ Key, Value string }

// RecordingKey is the recording a transcript came from. A file carrying it can
// be traced back to its source by reading it, which is what lets a directory be
// scanned for what has already been filed instead of a list of it being kept.
const RecordingKey = "recording"

var scalarLine = regexp.MustCompile(`^([A-Za-z_][\w-]*): `)

// WriteFields sets scalar keys in a transcript's front matter, leaving every
// other line of it alone and keeping the voices block last.
func WriteFields(content string, fields []Field) string {
	if len(fields) == 0 {
		return content
	}

	inner, rest := splitFrontMatter(content)
	scalars, voices := splitVoices(inner)

	for _, f := range fields {
		scalars = setField(scalars, f)
	}

	body := strings.Join(scalars, "\n")
	if body != "" {
		body += "\n"
	}
	return "---\n" + body + voices + "---\n" + rest
}

// ReadField takes one scalar back out of the front matter.
func ReadField(content, key string) string {
	inner, _ := splitFrontMatter(content)
	for _, line := range strings.Split(inner, "\n") {
		if name, value, found := strings.Cut(line, ": "); found && name == key {
			return strings.Trim(strings.TrimSpace(value), `"`)
		}
	}
	return ""
}

// Fields renders a declared map in a stable order, so two runs of the same
// configuration produce the same file.
func Fields(declared map[string]string) []Field {
	keys := make([]string, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	fields := make([]Field, 0, len(keys))
	for _, key := range keys {
		fields = append(fields, Field{Key: key, Value: declared[key]})
	}
	return fields
}

func splitFrontMatter(content string) (inner, rest string) {
	found := frontMatter.FindStringSubmatchIndex(content)
	if found == nil {
		return "", "\n" + content
	}
	return content[found[2]:found[3]], content[found[1]:]
}

// splitVoices separates the scalar lines from the voices block, which is a
// block rather than a scalar and always goes last.
func splitVoices(inner string) (scalars []string, voices string) {
	var block []string
	inside := false
	for _, line := range strings.Split(inner, "\n") {
		switch {
		case strings.HasPrefix(line, voicesKey):
			inside = true
			block = append(block, line)
		case inside && strings.HasPrefix(line, "  "):
			block = append(block, line)
		case line == "":
		default:
			inside = false
			scalars = append(scalars, line)
		}
	}
	if len(block) == 0 {
		return scalars, ""
	}
	return scalars, strings.Join(block, "\n") + "\n"
}

func setField(lines []string, f Field) []string {
	written := f.Key + ": " + quoted(f.Value)
	for i, line := range lines {
		if name := scalarLine.FindStringSubmatch(line); name != nil && name[1] == f.Key {
			lines[i] = written
			return lines
		}
	}
	return append(lines, written)
}

// quoted writes a value the way YAML reads it back unchanged. A meeting title
// opens with a date more often than not, and a bare 2026-08-20 is a date to a
// YAML reader rather than the string it was written as.
func quoted(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, `:#"'{}[],&*?|<>=!%@`+"`\n") || strings.HasPrefix(value, " ") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(value) + `"`
	}
	return value
}
