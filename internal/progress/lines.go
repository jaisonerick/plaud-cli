package progress

import (
	"fmt"
	"io"
	"sync"
	"time"
)

// stillGoing is how long a stage may run without saying anything. A log that
// goes quiet for the length of a GPU pass reads the same as one that stopped.
const stillGoing = 15 * time.Second

// lines is the display for output nobody is watching: one line per thing that
// happened, in the order it happened, with nothing redrawn.
type lines struct {
	out io.Writer

	mu       sync.Mutex
	labels   map[string]string
	started  map[string]time.Time
	lastSaid map[string]time.Time
	done     map[string]bool
	order    []string
}

func newLines(w io.Writer, stages []StageDef) *lines {
	l := &lines{
		out:      w,
		labels:   map[string]string{},
		started:  map[string]time.Time{},
		lastSaid: map[string]time.Time{},
		done:     map[string]bool{},
	}
	l.AddStages(stages)
	return l
}

func (l *lines) AddStages(stages []StageDef) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range stages {
		if _, known := l.labels[s.ID]; !known {
			l.labels[s.ID] = s.Label
			l.order = append(l.order, s.ID)
		}
	}
}

func (l *lines) Update(evt Event) {
	l.mu.Lock()
	defer l.mu.Unlock()

	label, known := l.labels[evt.Stage]
	if !known {
		return
	}

	switch evt.Status {
	case "started":
		l.started[evt.Stage] = time.Now()
		l.lastSaid[evt.Stage] = time.Now()
		l.say("· %s%s", label, detail(evt))

	case "progress":
		if _, running := l.started[evt.Stage]; !running {
			l.started[evt.Stage] = time.Now()
			l.say("· %s", label)
		}
		// Every event would be a line every hundred milliseconds, and none of
		// them would be read. What a log needs is proof it is still moving.
		if time.Since(l.lastSaid[evt.Stage]) < stillGoing {
			return
		}
		l.lastSaid[evt.Stage] = time.Now()
		l.say("  %s%s%s", label, progressed(evt), detail(evt))

	case "done":
		l.done[evt.Stage] = true
		l.say("✓ %s%s  %s", label, detail(evt), l.elapsed(evt.Stage))

	case "skipped":
		l.done[evt.Stage] = true
		l.say("⊘ %s skipped", label)
	}
}

// Abort says which stages never finished, which is what a run that fell over
// halfway leaves behind and what a reader of the log has to know.
func (l *lines) Abort() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, id := range l.order {
		if _, running := l.started[id]; running && !l.done[id] {
			l.done[id] = true
			l.say("⊘ %s did not finish", l.labels[id])
		}
	}
}

// Wait has nothing to wait for: every line was written when it happened.
func (l *lines) Wait() {}

func (l *lines) say(format string, args ...any) {
	fmt.Fprintf(l.out, format+"\n", args...)
}

func (l *lines) elapsed(stage string) string {
	at, running := l.started[stage]
	if !running {
		return ""
	}
	return formatElapsed(time.Since(at))
}

func progressed(evt Event) string {
	if evt.Total <= 0 {
		return ""
	}
	return fmt.Sprintf("  %d/%d", evt.Current, evt.Total)
}

func detail(evt Event) string {
	if evt.Detail == "" {
		return ""
	}
	return "  " + evt.Detail
}
