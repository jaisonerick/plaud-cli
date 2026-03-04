package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	searchLimit   int
	searchNoCache bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search across all transcripts",
	Long: `Search for text across all recording transcripts.

Transcripts are cached locally for fast subsequent searches.

Examples:
  plaud search "project deadline"
  plaud search "budget" --limit 5
  plaud search "meeting" --no-cache`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		query := args[0]

		// Fetch all recordings
		recordings, err := client.ListRecordings(ctx)
		if err != nil {
			return err
		}

		cacheDir, err := transcriptCacheDir()
		if err != nil {
			return fmt.Errorf("determining cache directory: %w", err)
		}

		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return fmt.Errorf("creating cache directory: %w", err)
		}

		totalMatches := 0
		matchedRecordings := 0

		for _, r := range recordings {
			if !r.HasTranscript {
				continue
			}

			// Try cache first
			segments, err := loadCachedTranscript(cacheDir, r.ID)
			if err != nil || searchNoCache {
				// Fetch from API
				detail, err := client.GetDetail(ctx, r.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not fetch details for %s: %v\n", r.Name, err)
					continue
				}

				url := detail.TranscriptURL()
				if url == "" {
					continue
				}

				data, err := client.FetchGzipped(ctx, url)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not download transcript for %s: %v\n", r.Name, err)
					continue
				}

				segments, err = transcript.Parse(data)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not parse transcript for %s: %v\n", r.Name, err)
					continue
				}

				// Cache the transcript
				_ = cacheTranscript(cacheDir, r.ID, data)
			}

			matches := transcript.Search(segments, query)
			if len(matches) == 0 {
				continue
			}

			matchedRecordings++
			if matchedRecordings > 1 {
				fmt.Println()
			}

			fmt.Printf("%s (%s)\n", r.Name, api.FormatEpochMs(r.StartTime))
			for _, m := range matches {
				totalMatches++
				content := m.Segment.Content
				if len(content) > 120 {
					content = content[:117] + "..."
				}
				fmt.Printf("  [%s] \"%s\"\n", transcript.FormatTimestamp(m.Segment.StartTime), content)
			}

			if searchLimit > 0 && matchedRecordings >= searchLimit {
				break
			}
		}

		if totalMatches == 0 {
			fmt.Println("No matches found.")
		} else {
			fmt.Printf("\n%d match(es) across %d recording(s)\n", totalMatches, matchedRecordings)
		}

		return nil
	},
}

func transcriptCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "plaud", "cache", "transcripts"), nil
}

func loadCachedTranscript(cacheDir, id string) ([]transcript.Segment, error) {
	data, err := os.ReadFile(filepath.Join(cacheDir, id+".json"))
	if err != nil {
		return nil, err
	}
	return transcript.Parse(data)
}

func cacheTranscript(cacheDir, id string, data []byte) error {
	return os.WriteFile(filepath.Join(cacheDir, id+".json"), data, 0644)
}

// fetchTranscriptSegments downloads and parses a transcript for a recording.
// Uses cache if available and caching is enabled.
func fetchTranscriptSegments(r api.RecordingSimple, useCache bool) ([]transcript.Segment, error) {
	cacheDir, err := transcriptCacheDir()
	if err != nil {
		return nil, err
	}

	if useCache {
		if segments, err := loadCachedTranscript(cacheDir, r.ID); err == nil {
			return segments, nil
		}
	}

	return nil, fmt.Errorf("not cached")
}

func init() {
	searchCmd.Flags().IntVar(&searchLimit, "limit", 0, "limit number of recordings to show")
	searchCmd.Flags().BoolVar(&searchNoCache, "no-cache", false, "bypass transcript cache")
	rootCmd.AddCommand(searchCmd)
}
