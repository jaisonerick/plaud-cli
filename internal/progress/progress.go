package progress

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
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
	bar       *mpb.Bar
	detail    atomic.Value // stores string
	started   atomic.Bool
	done      atomic.Bool
	startedAt atomic.Value // stores time.Time
}

func (s *stageState) getDetail() string {
	v := s.detail.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

func (s *stageState) getElapsed() string {
	v := s.startedAt.Load()
	if v == nil {
		return ""
	}
	d := time.Since(v.(time.Time))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Tracker manages the multi-bar progress display.
type Tracker struct {
	p         *mpb.Progress
	stages    map[string]*stageState
	startTime time.Time
	output    io.Writer
}

// NewTracker creates a progress tracker with the given initial stages.
func NewTracker(w io.Writer, stages []StageDef) *Tracker {
	p := mpb.New(
		mpb.WithOutput(w),
		mpb.WithRefreshRate(100*time.Millisecond),
	)

	t := &Tracker{
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
func (t *Tracker) AddStages(stages []StageDef) {
	for _, s := range stages {
		if _, exists := t.stages[s.ID]; !exists {
			t.addBar(s)
		}
	}
}

func (t *Tracker) addBar(def StageDef) {
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
				if statistics.Completed || st.done.Load() {
					return detail
				}
				// While active: show detail + elapsed time
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
func (t *Tracker) Update(evt Event) {
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

// Wait blocks until all bars have completed rendering.
func (t *Tracker) Wait() {
	t.p.Wait()
}

func spinnerFrames() []string {
	return []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
}
