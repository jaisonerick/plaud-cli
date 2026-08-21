package transcript

import (
	"regexp"
	"strconv"
	"strings"
)

// Turn is one stretch of a written transcript held by one voice, as the file
// lays it out: who it says is speaking, when it starts, and the line that says
// so, which is what makes the name replaceable without rendering the file anew.
type Turn struct {
	Line    int
	Speaker string
	StartMS int
	EndMS   int
}

// lastTurnMS is how long the final turn is taken to run for. The file records
// where a turn starts and the next one takes it from there, so only the last
// one has no end.
const lastTurnMS = 30000

var turnHeader = regexp.MustCompile(`^\*\*(.+?)\*\*\s*\((\d{2}):(\d{2}):(\d{2})\)\s*:`)

// ReadTurns finds the turns of a markdown transcript this tool wrote.
func ReadTurns(content string) []Turn {
	var turns []Turn
	for i, line := range strings.Split(content, "\n") {
		fields := turnHeader.FindStringSubmatch(line)
		if fields == nil {
			continue
		}
		hours, _ := strconv.Atoi(fields[2])
		minutes, _ := strconv.Atoi(fields[3])
		seconds, _ := strconv.Atoi(fields[4])
		turns = append(turns, Turn{
			Line:    i,
			Speaker: fields[1],
			StartMS: (hours*3600 + minutes*60 + seconds) * 1000,
		})
	}
	for i := range turns {
		if i+1 < len(turns) {
			turns[i].EndMS = turns[i+1].StartMS
		} else {
			turns[i].EndMS = turns[i].StartMS + lastTurnMS
		}
	}
	return turns
}

// RewriteSpeakers puts a new name on the given turns and leaves the rest of the
// file byte for byte as it was: a transcript gains frontmatter, headings and
// corrections after it is written, and rendering it again would drop them.
func RewriteSpeakers(content string, names map[int]string) (string, int) {
	lines := strings.Split(content, "\n")
	renamed := 0
	for line, name := range names {
		if line < 0 || line >= len(lines) {
			continue
		}
		fields := turnHeader.FindStringSubmatch(lines[line])
		if fields == nil || fields[1] == name {
			continue
		}
		lines[line] = strings.Replace(lines[line], "**"+fields[1]+"**", "**"+name+"**", 1)
		renamed++
	}
	if renamed == 0 {
		return content, 0
	}
	return strings.Join(lines, "\n"), renamed
}
