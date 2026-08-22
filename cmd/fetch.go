package cmd

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/catalog"
	"github.com/jaisonerick/plaud-cli/internal/modal"
	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	fetchTo          string
	fetchSummaryTo   string
	fetchProfile     string
	fetchContext     string
	fetchContextFile string
	fetchLanguage    string
	fetchFormat      string
	fetchForce       bool
)

var fetchCmd = &cobra.Command{
	Use:   "fetch <id>",
	Short: "Bring one recording into this repository, where it declares transcripts go",
	Long: `Put one recording's transcript where this repository puts transcripts.

The destination, what the file is called, what its front matter carries and
what describes the recording are read from ` + repo.FileName + ` rather than
passed on the call. 'plaud config' prints what was resolved.

A transcript is made when nobody has one yet, and handed back in seconds when
the service already decoded it. A transcript already at the destination is not
fetched again: the names in it are settled against the voices as they are known
today, which is what a voice named since is worth doing.

The summary comes along when Plaud has one, beside the transcript, and a
repository keeping a catalog has this recording's entry brought up to date.

Examples:
  plaud fetch abc123
  plaud fetch abc123 --context "CERC x Vexia; Éricles Bento (CERC), Luana (Vexia)"
  plaud fetch abc123 --to comms/2026-08-06/transcript.md
  plaud fetch abc123 --profile cerc`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		e, err := newErrand(fetchProfile, fetchContext, fetchContextFile, fetchFormat, fetchLanguage, fetchForce)
		if err != nil {
			return err
		}
		e.to, e.summaryTo = fetchTo, fetchSummaryTo

		detail, err := client.GetDetail(ctx, args[0])
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}
		recording := api.RecordingSimple{
			ID: detail.ID, Name: detail.Name, StartTime: detail.StartTime,
			Duration: detail.Duration, HasTranscript: detail.HasTranscript(), HasSummary: detail.HasSummary(),
		}

		whisper, err := whisperClient()
		if err != nil {
			return err
		}

		one := report{Recording: recording.ID}
		if jsonOut {
			held = &one
			defer func() { held = nil }()
		}

		dest, summary, err := e.run(ctx, whisper, recording)
		if err != nil {
			return err
		}

		if jsonOut {
			one.Recording = recording.ID
			one.Path = dest
			one.Summary = summary
			one.Filing = e.repo.Filing
			printReport(one)
			return nil
		}

		fmt.Printf("transcript: %s\n", e.repo.Rel(dest))
		if summary != "" {
			fmt.Printf("summary:    %s\n", e.repo.Rel(summary))
		}
		if e.repo.Filing != "" {
			fmt.Printf("\nWhere this belongs in this repository: %s\n", e.repo.Filing)
		}
		return nil
	},
}

// errand is what this repository does with a recording: where the transcript
// goes, what describes it, and how it is written. It is settled once and used
// for one recording or for a hundred.
type errand struct {
	repo        *repo.Config
	profile     repo.Profile
	description string
	how         filing

	// to and summaryTo override the repository, and are how a caller files one
	// recording somewhere the configuration does not describe.
	to        string
	summaryTo string
}

func newErrand(profileName, text, file, format, language string, force bool) (*errand, error) {
	r, err := repository()
	if err != nil {
		return nil, err
	}
	profile, err := chooseProfile(r, profileName)
	if err != nil {
		return nil, err
	}
	if err := validateFormat(format); err != nil {
		return nil, err
	}

	// Before anything is fetched: a description that cannot be read is worth
	// saying so about while it still costs nothing.
	description, err := describe(r, profile, text, file)
	if err != nil {
		return nil, err
	}

	settled := first(language, profile.Language, r.Language)
	return &errand{
		repo:        r,
		profile:     profile,
		description: description,
		how: filing{
			format: format,
			force:  force,
			opts:   modal.TranscribeOpts{ContextDoc: description, Language: settled, Force: force},
		},
	}, nil
}

func (e *errand) run(ctx context.Context, whisper *modal.HTTPClient, recording api.RecordingSimple) (dest, summary string, err error) {
	dest, err = e.destination(recording)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return "", "", fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}

	if err := newJob(recording, dest, e.how, true).run(ctx, whisper); err != nil {
		return "", "", err
	}
	if err := e.stamp(dest, recording); err != nil {
		return "", "", err
	}
	summary, err = e.fetchSummary(ctx, dest, recording)
	if err != nil {
		return "", "", err
	}
	return dest, summary, e.record(recording, dest, summary)
}

// destination is the file this transcript belongs in: what a caller named, or
// what the repository's templates work out.
func (e *errand) destination(recording api.RecordingSimple) (string, error) {
	ext := transcript.Ext(e.how.format)
	subject := repo.Recording{ID: recording.ID, Name: recording.Name, Start: recording.StartTime}

	if e.to == "" {
		return e.repo.Target(e.profile, subject, ext)
	}
	if filepath.Ext(e.to) != "" {
		return e.repo.Abs(e.to), nil
	}
	// A directory keeps the repository's naming; only the place changed.
	named := repo.Config{Root: e.repo.Root, Dest: e.to, Name: e.repo.Name, UTCOffset: e.repo.UTCOffset}
	return named.Target(e.profile, subject, ext)
}

// chooseProfile picks the named set of rules, and refuses a name the
// repository does not declare rather than filing against the defaults.
func chooseProfile(r *repo.Config, name string) (repo.Profile, error) {
	if name == "" {
		return repo.Profile{}, nil
	}
	profile, ok := r.Profile(name)
	if !ok {
		declared := "none"
		if names := r.ProfileNames(); len(names) > 0 {
			declared = strings.Join(names, ", ")
		}
		return repo.Profile{}, fmt.Errorf("%s declares no profile %q (it declares: %s)", repo.FileName, name, declared)
	}
	return profile, nil
}

// describe composes what the polisher is told about the recording.
//
// The repository's document holds the project's people and how their names,
// companies and systems are spelt; a description written for one recording
// holds who was in that room. They know different things, so passing one adds
// to the other rather than replacing it. --context-file is the exception: a
// recording described by a paper of its own stands alone.
func describe(r *repo.Config, p repo.Profile, text, file string) (string, error) {
	if file != "" && text != "" {
		return "", fmt.Errorf("--context and --context-file say the same thing twice; pass one")
	}

	document := file
	if document == "" {
		document = first(r.Abs(p.Context), r.Context)
	}
	if document == "" && text == "" {
		return "", fmt.Errorf("%s", noDescription)
	}

	var written string
	if document != "" {
		data, err := os.ReadFile(document)
		if err != nil {
			return "", fmt.Errorf("reading what describes this recording: %w", err)
		}
		written = strings.TrimRight(string(data), "\n")
	}
	if text != "" {
		if _, err := os.Stat(text); err == nil {
			return "", fmt.Errorf("%q is a file that exists, and --context is the description itself — pass --context-file to read it", text)
		}
		if written != "" {
			written += "\n\n"
		}
		written += strings.TrimSpace(text) + "\n"
	}
	return written, nil
}

// stamp writes what the repository files a transcript by, and the recording it
// came from. The recording is what makes a destination answer for itself: a
// directory can be read for what has already been filed, so nothing has to
// remember it.
func (e *errand) stamp(dest string, recording api.RecordingSimple) error {
	if e.how.format != "md" {
		return nil
	}
	content, err := os.ReadFile(dest)
	if err != nil {
		// A recording holding no speech leaves no file, and that is said
		// where it happens rather than failing here.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	declared := map[string]string{}
	for key, value := range e.repo.FrontMatter {
		declared[key] = value
	}
	for key, value := range e.profile.FrontMatter {
		declared[key] = value
	}
	fields := append(transcript.Fields(declared), transcript.Field{Key: transcript.RecordingKey, Value: recording.ID})

	return os.WriteFile(dest, []byte(transcript.WriteFields(string(content), fields)), 0644)
}

// fetchSummary copies the summary Plaud wrote, when it wrote one. Naming an
// exact file names the transcript and only the transcript.
func (e *errand) fetchSummary(ctx context.Context, dest string, recording api.RecordingSimple) (string, error) {
	if !recording.HasSummary {
		return "", nil
	}

	target := e.summaryTo
	switch {
	case target != "":
		target = e.repo.Abs(target)
	case e.to != "" && filepath.Ext(e.to) != "":
		return "", nil
	default:
		ext := filepath.Ext(dest)
		target = strings.TrimSuffix(dest, ext) + "-summary" + ext
	}

	detail, err := client.GetDetail(ctx, recording.ID)
	if err != nil {
		return "", fmt.Errorf("fetching recording details: %w", err)
	}
	url := detail.SummaryURL()
	if url == "" {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return "", err
	}
	if err := client.DownloadGzipped(ctx, url, target); err != nil {
		return "", fmt.Errorf("downloading summary: %w", err)
	}
	return target, nil
}

// record brings this recording's catalog entry up to date, in a repository
// that keeps one. Fetching without the catalog noticing is what left entries
// saying a recording had no transcript while the file sat beside them.
func (e *errand) record(recording api.RecordingSimple, dest, summary string) error {
	if !e.repo.KeepsCatalog() {
		return nil
	}
	if _, err := os.Stat(dest); err != nil {
		return nil
	}

	c, err := catalog.Open(e.repo.Hub)
	if err != nil {
		return err
	}
	entry, known := c.Get(recording.ID)
	if !known {
		entry = &catalog.Entry{
			ID: recording.ID, Filename: recording.Name, StartTime: recording.StartTime,
			RecordedAt:  e.repo.LocalTime(recording.StartTime).Format("2006-01-02 15:04:05"),
			DurationMS:  recording.Duration,
			DurationMin: math.Round(float64(recording.Duration)/600) / 100,
			URL:         "https://web.plaud.ai/file/" + recording.ID,
		}
		c.Put(entry)
	}
	entry.TranscriptPath = catalog.Text(e.repo.Rel(dest))
	if summary != "" {
		entry.SummaryPath = catalog.Text(e.repo.Rel(summary))
	}
	if entry.Recomputed() {
		entry.Status = catalog.Transcribed
	}
	return c.Save()
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

const noDescription = `nothing describes this recording, and a transcript needs it: it is what settles
how the names in it are spelt.

  Pass it now:  --context "Reunião X entre Fulano (Empresa) e Beltrano (Empresa); assunto"
  Or for good:  set "context" in ` + repo.FileName + ` to a file describing this work —
                a briefing, an agenda, the people and companies involved.

Name the people and companies that are actually in the recording. A description
about other work makes the polisher write those names over the ones being said.`

func init() {
	f := fetchCmd.Flags()
	f.StringVar(&fetchTo, "to", "", "a file, or a directory, overriding where this repository puts transcripts")
	f.StringVar(&fetchSummaryTo, "summary-to", "", "where the summary goes")
	f.StringVar(&fetchProfile, "profile", "", "the set of rules in "+repo.FileName+" to file this under")
	f.StringVar(&fetchContext, "context", "", "who was in this room and what it is about, added to the repository's document")
	f.StringVar(&fetchContextFile, "context-file", "", "a file describing this recording, standing in for the repository's document")
	f.StringVar(&fetchLanguage, "language", "", "settle the language (e.g. pt), which also decodes again")
	f.StringVar(&fetchFormat, "format", "md", "output format: json, txt, srt, md")
	f.BoolVar(&fetchForce, "force", false, "transcribe the audio again instead of reusing a transcript that exists")
	rootCmd.AddCommand(fetchCmd)
}
