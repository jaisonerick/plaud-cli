package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	syncDir        string
	syncAudio      bool
	syncTranscript bool
	syncSummary    bool
	syncFormat     string
	syncForce      bool
)

// syncState tracks which recordings have been synced.
type syncState struct {
	Recordings map[string]syncEntry `json:"recordings"`
}

type syncEntry struct {
	SyncedAt int64 `json:"synced_at"` // epoch seconds
	EditTime int64 `json:"edit_time"` // epoch seconds from API
}

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync recordings to a local directory",
	Long: `Download all recordings to a local directory, tracking changes.

Only new or updated recordings are downloaded on subsequent runs.

Examples:
  plaud sync                                    # Sync transcripts (md) + summaries
  plaud sync --dir ./recordings --audio         # Include audio files
  plaud sync --format txt                       # Use plain text format
  plaud sync --force                            # Re-download everything`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Default: transcript + summary
		if !syncAudio && !syncTranscript && !syncSummary {
			syncTranscript = true
			syncSummary = true
		}

		// Validate format
		switch syncFormat {
		case "json", "txt", "srt", "md":
			// ok
		default:
			return fmt.Errorf("unsupported format %q (use json, txt, srt, or md)", syncFormat)
		}

		// Ensure output directory exists
		if err := os.MkdirAll(syncDir, 0755); err != nil {
			return fmt.Errorf("creating sync directory: %w", err)
		}

		// Load sync state
		state, err := loadSyncState()
		if err != nil {
			return fmt.Errorf("loading sync state: %w", err)
		}

		// Fetch all recordings
		recordings, err := client.ListRecordings(ctx)
		if err != nil {
			return err
		}

		var synced, updated, upToDate, errors int

		for _, r := range recordings {
			entry, exists := state.Recordings[r.ID]

			// Skip if already synced and not updated (unless --force)
			if exists && !syncForce && entry.EditTime >= r.EditTime {
				upToDate++
				continue
			}

			// Create recording directory
			dirName := transcript.SanitizeFilename(r.Name) + "_" + strings.ReplaceAll(api.FormatEpochMs(r.StartTime), " ", "_")
			recDir := filepath.Join(syncDir, dirName)
			if err := os.MkdirAll(recDir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory for %s: %v\n", r.Name, err)
				errors++
				continue
			}

			isUpdate := exists
			hasError := false

			if syncAudio {
				fmt.Printf("  Downloading audio: %s... ", r.Name)
				tempURL, err := client.GetTempURL(ctx, r.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "error: %v\n", err)
					hasError = true
				} else {
					dest := filepath.Join(recDir, "audio.mp3")
					if err := client.DownloadFile(ctx, tempURL, dest); err != nil {
						fmt.Fprintf(os.Stderr, "error: %v\n", err)
						hasError = true
					} else {
						fmt.Println("done")
					}
				}
			}

			if syncTranscript && r.HasTranscript {
				detail, err := client.GetDetail(ctx, r.ID)
				if err != nil {
					fmt.Fprintf(os.Stderr, "  Error fetching details for %s: %v\n", r.Name, err)
					hasError = true
				} else if url := detail.TranscriptURL(); url != "" {
					fmt.Printf("  Downloading transcript: %s... ", r.Name)
					if syncFormat == "json" {
						dest := filepath.Join(recDir, "transcript.json")
						if err := client.DownloadGzipped(ctx, url, dest); err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							hasError = true
						} else {
							fmt.Println("done")
						}
					} else {
						data, err := client.FetchGzipped(ctx, url)
						if err != nil {
							fmt.Fprintf(os.Stderr, "error: %v\n", err)
							hasError = true
						} else {
							segments, err := transcript.Parse(data)
							if err != nil {
								fmt.Fprintf(os.Stderr, "error parsing: %v\n", err)
								hasError = true
							} else {
								ext, content := transcript.Format(segments, syncFormat)
								dest := filepath.Join(recDir, "transcript"+ext)
								if err := os.WriteFile(dest, []byte(content), 0644); err != nil {
									fmt.Fprintf(os.Stderr, "error writing: %v\n", err)
									hasError = true
								} else {
									fmt.Println("done")
								}
							}
						}
					}
				}
			}

			if syncSummary && r.HasSummary {
				detail, detailErr := func() (*api.RecordingDetail, error) {
					// Reuse detail if already fetched for transcript
					return client.GetDetail(ctx, r.ID)
				}()
				if detailErr != nil {
					fmt.Fprintf(os.Stderr, "  Error fetching details for %s: %v\n", r.Name, detailErr)
					hasError = true
				} else if url := detail.SummaryURL(); url != "" {
					fmt.Printf("  Downloading summary: %s... ", r.Name)
					dest := filepath.Join(recDir, "summary.md")
					if err := client.DownloadGzipped(ctx, url, dest); err != nil {
						fmt.Fprintf(os.Stderr, "error: %v\n", err)
						hasError = true
					} else {
						fmt.Println("done")
					}
				}
			}

			if hasError {
				errors++
			} else {
				// Update state
				state.Recordings[r.ID] = syncEntry{
					SyncedAt: time.Now().Unix(),
					EditTime: r.EditTime,
				}
				if isUpdate {
					updated++
				} else {
					synced++
				}
			}
		}

		// Save state
		if err := saveSyncState(state); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not save sync state: %v\n", err)
		}

		fmt.Printf("\nSynced %d new, %d updated, %d up to date", synced, updated, upToDate)
		if errors > 0 {
			fmt.Printf(", %d errors", errors)
		}
		fmt.Println()

		return nil
	},
}

func syncStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "plaud", "sync-state.json"), nil
}

func loadSyncState() (*syncState, error) {
	p, err := syncStatePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return &syncState{Recordings: make(map[string]syncEntry)}, nil
		}
		return nil, err
	}

	var s syncState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	if s.Recordings == nil {
		s.Recordings = make(map[string]syncEntry)
	}
	return &s, nil
}

func saveSyncState(s *syncState) error {
	p, err := syncStatePath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(p, data, 0644)
}

func init() {
	syncCmd.Flags().StringVar(&syncDir, "dir", "./plaud-recordings", "output directory")
	syncCmd.Flags().BoolVar(&syncAudio, "audio", false, "sync audio files")
	syncCmd.Flags().BoolVar(&syncTranscript, "transcript", false, "sync transcripts")
	syncCmd.Flags().BoolVar(&syncSummary, "summary", false, "sync summaries")
	syncCmd.Flags().StringVar(&syncFormat, "format", "md", "transcript format: json, txt, srt, md")
	syncCmd.Flags().BoolVar(&syncForce, "force", false, "re-download everything")
	rootCmd.AddCommand(syncCmd)
}
