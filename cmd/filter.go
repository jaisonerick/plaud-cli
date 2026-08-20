package cmd

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/spf13/cobra"
)

// recordingFilter narrows an account's recordings to the ones a command should
// act on. Every command that can act on more than one builds one of these from
// the same flags, so a filter learnt on `list` selects the same recordings for
// the command that acts on them.
type recordingFilter struct {
	tag           string
	since         string
	before        string
	search        string
	limit         int
	hasTranscript bool
	hasSummary    bool
}

func addFilterFlags(cmd *cobra.Command, f *recordingFilter) {
	cmd.Flags().StringVar(&f.tag, "tag", "", "only recordings carrying this tag")
	cmd.Flags().StringVar(&f.since, "since", "", "only recordings from this date on (YYYY-MM-DD)")
	cmd.Flags().StringVar(&f.before, "before", "", "only recordings up to this date (YYYY-MM-DD)")
	cmd.Flags().StringVar(&f.search, "search", "", "only recordings whose name contains this")
	cmd.Flags().IntVar(&f.limit, "limit", 0, "stop after this many recordings")
	cmd.Flags().BoolVar(&f.hasTranscript, "has-transcript", false, "only recordings Plaud already transcribed")
	cmd.Flags().BoolVar(&f.hasSummary, "has-summary", false, "only recordings Plaud already summarized")
}

// selects reports whether the caller narrowed anything at all. A command that
// acts on every recording it is given asks this before deciding it was told to.
func (f recordingFilter) selects() bool {
	return f.tag != "" || f.since != "" || f.before != "" || f.search != "" ||
		f.limit > 0 || f.hasTranscript || f.hasSummary
}

// apply keeps the recordings the filter names, in the order they arrived.
func (f recordingFilter) apply(recordings []api.RecordingSimple, tagNameToID map[string]string) []api.RecordingSimple {
	if !f.selects() {
		return recordings
	}

	var tagID string
	if f.tag != "" {
		id, ok := tagNameToID[strings.ToLower(f.tag)]
		if !ok {
			return nil
		}
		tagID = id
	}

	var since, before time.Time
	if f.since != "" {
		since, _ = time.Parse("2006-01-02", f.since)
	}
	if f.before != "" {
		before, _ = time.Parse("2006-01-02", f.before)
	}
	search := strings.ToLower(f.search)

	var kept []api.RecordingSimple
	for _, r := range recordings {
		if tagID != "" && !hasTag(r, tagID) {
			continue
		}
		at := time.Unix(0, r.StartTime*int64(time.Millisecond))
		if !since.IsZero() && at.Before(since) {
			continue
		}
		if !before.IsZero() && at.After(before.Add(24*time.Hour)) {
			continue
		}
		if f.hasTranscript && !r.HasTranscript {
			continue
		}
		if f.hasSummary && !r.HasSummary {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(r.Name), search) {
			continue
		}

		kept = append(kept, r)
		if f.limit > 0 && len(kept) >= f.limit {
			break
		}
	}
	return kept
}

func hasTag(r api.RecordingSimple, tagID string) bool {
	for _, id := range r.Tags {
		if id == tagID {
			return true
		}
	}
	return false
}

// tagNames maps tag ids to names and back. A failure to read them is not worth
// stopping for: it costs the --tag filter and the tag column, not the command.
func tagNames(ctx context.Context) (byID, byName map[string]string) {
	byID, byName = map[string]string{}, map[string]string{}
	tags, _ := client.ListTags(ctx)
	for _, t := range tags {
		byID[t.ID] = t.Name
		byName[strings.ToLower(t.Name)] = t.ID
	}
	return byID, byName
}

// chooseRecordings resolves what a command was pointed at: one recording named
// by id, or every recording a filter keeps. Being told neither is an error,
// because the alternative is a command that silently means "all of them".
func chooseRecordings(ctx context.Context, args []string, f recordingFilter, all bool) ([]api.RecordingSimple, error) {
	if len(args) > 0 {
		detail, err := client.GetDetail(ctx, args[0])
		if err != nil {
			return nil, fmt.Errorf("fetching recording details: %w", err)
		}
		return []api.RecordingSimple{{
			ID:            detail.ID,
			Name:          detail.Name,
			StartTime:     detail.StartTime,
			Duration:      detail.Duration,
			HasTranscript: detail.HasTranscript(),
			HasSummary:    detail.HasSummary(),
		}}, nil
	}

	if !all && !f.selects() {
		return nil, fmt.Errorf("name a recording, or pass --all or a filter such as --since")
	}

	recordings, err := client.ListRecordings(ctx)
	if err != nil {
		return nil, err
	}
	_, byName := tagNames(ctx)
	return f.apply(recordings, byName), nil
}
