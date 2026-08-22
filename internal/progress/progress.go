package progress

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/term"
)

// StageDef defines a stage for the progress display.
// Server stages arrive dynamically via the init event; client stages are
// created by the command layer.
type StageDef struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Event represents a progress update for a stage.
type Event struct {
	Stage   string `json:"stage"`
	Status  string `json:"status"`  // "started", "progress", "done", "skipped"
	Current int    `json:"current"` // For progress: current item
	Total   int    `json:"total"`   // For progress: total items
	Detail  string `json:"detail"`  // Right-side text
}

// stageState uses atomic values to avoid holding a mutex in decorator callbacks
// (which run under mpb's internal lock).
type stageState struct {
	bar          *mpb.Bar
	detail       atomic.Value // stores string
	started      atomic.Bool
	done         atomic.Bool
	startedAt    atomic.Value // stores time.Time
	finalElapsed atomic.Value // stores string — frozen elapsed on done
}

func (s *stageState) getDetail() string {
	v := s.detail.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func (s *stageState) getElapsed() string {
	// If done, return the frozen elapsed time
	if v := s.finalElapsed.Load(); v != nil {
		return v.(string)
	}
	v := s.startedAt.Load()
	if v == nil {
		return ""
	}
	return formatElapsed(time.Since(v.(time.Time)))
}

func formatElapsed(d time.Duration) string {
	if d < 10*time.Second {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Tracker reports how far a job has got. There are two, and which one runs is
// decided by where the output goes: bars redraw themselves and need a terminal
// to do it, and this command runs unwatched far more often than not, where a
// bar that cannot redraw prints nothing at all. A transcription is minutes of
// silence then, with no way to tell a slow run from a hung one.
type Tracker interface {
	Update(Event)
	AddStages([]StageDef)
	Abort()
	Wait()
}

// NewTracker picks the display the output can carry.
func NewTracker(w io.Writer, stages []StageDef) Tracker {
	if isTerminal(w) {
		return newBars(w, stages)
	}
	return newLines(w, stages)
}

func isTerminal(w io.Writer) bool {
	file, ok := w.(*os.File)
	return ok && term.IsTerminal(int(file.Fd()))
}

// bars is the terminal display: one line per stage, redrawn in place.
type bars struct {
	p         *mpb.Progress
	stages    map[string]*stageState
	startTime time.Time
	output    io.Writer
}

func newBars(w io.Writer, stages []StageDef) *bars {
	p := mpb.New(
		mpb.WithOutput(w),
		mpb.WithRefreshRate(100*time.Millisecond),
	)

	t := &bars{
		p:         p,
		stages:    make(map[string]*stageState),
		startTime: time.Now(),
		output:    w,
	}

	for _, s := range stages {
		t.addBar(s)
	}

	return t
}

// AddStages appends new stages to the display. Called when the server's
// init event declares its pipeline stages.
func (t *bars) AddStages(stages []StageDef) {
	for _, s := range stages {
		if _, exists := t.stages[s.ID]; !exists {
			t.addBar(s)
		}
	}
}

func (t *bars) addBar(def StageDef) {
	st := &stageState{}
	t.stages[def.ID] = st

	frames := spinnerFrames()
	startTime := t.startTime

	bar := t.p.AddBar(0,
		mpb.BarWidth(1),
		mpb.BarFillerClearOnComplete(),
		mpb.PrependDecorators(
			decor.Any(func(statistics decor.Statistics) string {
				if statistics.Aborted {
					return "⊘"
				}
				if statistics.Completed {
					return "✓"
				}
				if !st.started.Load() {
					return " "
				}
				elapsed := time.Since(startTime)
				idx := int(elapsed.Milliseconds()/100) % len(frames)
				return frames[idx]
			}, decor.WCSyncSpaceR),
			decor.Name(def.Label, decor.WCSyncWidthR),
		),
		mpb.AppendDecorators(
			decor.Any(func(statistics decor.Statistics) string {
				if statistics.Aborted {
					return "skipped"
				}
				detail := st.getDetail()
				elapsed := st.getElapsed()
				if detail != "" && elapsed != "" {
					return detail + "  " + elapsed
				}
				if elapsed != "" {
					return elapsed
				}
				return detail
			}),
		),
	)
	st.bar = bar
}

// Update processes a progress event and updates the display.
func (t *bars) Update(evt Event) {
	st, ok := t.stages[evt.Stage]
	if !ok {
		return
	}

	switch evt.Status {
	case "started":
		if !st.started.Load() {
			st.started.Store(true)
			st.startedAt.Store(time.Now())
			if evt.Total > 0 {
				st.bar.SetTotal(int64(evt.Total), false)
			}
		}
		if evt.Detail != "" {
			st.detail.Store(evt.Detail)
		}

	case "progress":
		if !st.started.Load() {
			st.started.Store(true)
			st.startedAt.Store(time.Now())
		}
		if evt.Total > 0 {
			st.bar.SetTotal(int64(evt.Total), false)
		}
		if evt.Current > 0 {
			inc := int64(evt.Current) - st.bar.Current()
			if inc > 0 {
				st.bar.IncrBy(int(inc))
			}
		}
		if evt.Detail != "" {
			st.detail.Store(evt.Detail)
		} else if evt.Total > 0 {
			st.detail.Store(fmt.Sprintf("%d/%d", evt.Current, evt.Total))
		}

	case "done":
		if evt.Detail != "" {
			st.detail.Store(evt.Detail)
		}
		// Freeze elapsed time at completion
		if v := st.startedAt.Load(); v != nil {
			st.finalElapsed.Store(formatElapsed(time.Since(v.(time.Time))))
		}
		st.done.Store(true)
		if st.bar.Current() == 0 {
			st.bar.SetTotal(1, false)
			st.bar.IncrBy(1)
		}
		st.bar.SetTotal(st.bar.Current(), true)

	case "skipped":
		st.done.Store(true)
		st.bar.Abort(false)
	}
}

// Abort aborts all incomplete bars so that Wait() won't deadlock.
// Call this before Wait() in error paths.
func (t *bars) Abort() {
	for _, st := range t.stages {
		if !st.done.Load() {
			st.bar.Abort(true)
		}
	}
}

// Wait blocks until all bars have completed rendering.
func (t *bars) Wait() {
	t.p.Wait()
}

func spinnerFrames() []string {
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}
