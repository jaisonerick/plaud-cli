package transcript

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// VoiceBlock says which voice each name in a transcript stands for.
//
// A transcript writes people, because people are what anybody reads. Who those
// people are is settled the day it is written and can be settled better later,
// so the file also carries the id of the voice behind each name, and that is
// what a later run asks about. Names are a rendering; the ids are the record.
//
// One name can hold more than one voice: diarization splits a person in two as
// readily as it merges two into one.
type VoiceBlock map[string][]string

const voicesKey = "voices:"

var (
	frontMatter = regexp.MustCompile(`(?s)\A---\n(.*?)\n---\n`)
	voiceLine   = regexp.MustCompile(`^  "(.*)": \[(.*)\]$`)
)

// WriteVoices puts the block at the top of a transcript, under any front
// matter already there.
func WriteVoices(content string, voices VoiceBlock) string {
	if len(voices) == 0 {
		return content
	}

	block := renderVoices(voices)
	if found := frontMatter.FindStringSubmatchIndex(content); found != nil {
		inner := content[found[2]:found[3]]
		return "---\n" + stripVoices(inner) + block + "---\n" + content[found[1]:]
	}
	return "---\n" + block + "---\n\n" + content
}

// ReadVoices takes the block back, and returns nil for a transcript written
// before the ids existed, which then answers only for its labels.
func ReadVoices(content string) VoiceBlock {
	found := frontMatter.FindStringSubmatch(content)
	if found == nil {
		return nil
	}

	voices := VoiceBlock{}
	inside := false
	for _, line := range strings.Split(found[1], "\n") {
		if strings.HasPrefix(line, voicesKey) {
			inside = true
			continue
		}
		if inside && !strings.HasPrefix(line, "  ") {
			inside = false
		}
		if !inside {
			continue
		}
		fields := voiceLine.FindStringSubmatch(line)
		if fields == nil {
			continue
		}
		var ids []string
		for _, id := range strings.Split(fields[2], ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
		voices[fields[1]] = ids
	}
	if len(voices) == 0 {
		return nil
	}
	return voices
}

// Rename moves the voices of one name to another, keeping the block a record of
// which voice was written under which name.
func (v VoiceBlock) Rename(from, to string) {
	if from == to {
		return
	}
	v[to] = append(v[to], v[from]...)
	delete(v, from)
}

func renderVoices(voices VoiceBlock) string {
	names := make([]string, 0, len(voices))
	for name := range voices {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(voicesKey + "\n")
	for _, name := range names {
		fmt.Fprintf(&b, "  %q: [%s]\n", name, strings.Join(voices[name], ", "))
	}
	return b.String()
}

// stripVoices takes the block out of front matter, leaving whatever else it
// holds untouched.
func stripVoices(inner string) string {
	var kept []string
	inside := false
	for _, line := range strings.Split(inner, "\n") {
		if strings.HasPrefix(line, voicesKey) {
			inside = true
			continue
		}
		if inside && strings.HasPrefix(line, "  ") {
			continue
		}
		inside = false
		kept = append(kept, line)
	}
	trimmed := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
}
