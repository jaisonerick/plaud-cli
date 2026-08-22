package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/jaisonerick/plaud-cli/internal/repo"
	"github.com/spf13/cobra"
)

// project is the repository this run is filing into, read once. A command
// resolving it a second time would read the same file and could report a
// different root if it ran from somewhere else, which is exactly the confusion
// this replaces.
var project *repo.Config

// repository resolves the repository governing the working directory, and
// says once about any key in it that nothing reads.
func repository() (*repo.Config, error) {
	if project != nil {
		return project, nil
	}
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	project, err = repo.Find(dir)
	if err != nil {
		return nil, err
	}
	for _, key := range project.Unknown {
		fmt.Fprintf(os.Stderr, "warning: %s carries %q, which nothing reads\n", project.Rel(project.File), key)
	}
	return project, nil
}

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show what settles how this repository takes transcripts in",
	Long: `Print the resolved configuration of the repository this command runs in, and
which layer settled each part of it.

Three layers, general to specific: what you carry everywhere, what the
repository declares in ` + repo.FileName + ` at its root, and what you set about
this repository. The last wins, because it is the most specific and you wrote
it here. "set_in" names the layer behind each value.

The repository's file is found from the working directory upwards. One that
declares nothing is not broken: transcripts can still be fetched into a path
named on the call, and nothing here says where they belong.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		r, err := repository()
		if err != nil {
			return err
		}

		mode := "ad-hoc"
		if r.KeepsCatalog() {
			mode = "catalog"
		}
		profiles := map[string]any{}
		for _, name := range r.ProfileNames() {
			profile, _ := r.Profile(name)
			profiles[name] = map[string]any{
				"tag":        profile.Tag,
				"tag_set_in": r.Where("profiles." + name + ".tag"),
				"dest":       profile.Dest,
			}
		}
		resolved := map[string]any{
			"root":     r.Root,
			"declared": nil,
			"identity": r.Identity,
			"settings": r.Settings,
			"mode":     mode,
			"context":  r.Rel(r.Context),
			"filing":   r.Filing,
			"scratch":  r.Rel(r.Scratch),
			"hub":      r.Rel(r.Hub),
			"language": r.Language,
			"profiles": profiles,
			"set_in":   r.Sources(),
		}
		if r.Declares() {
			resolved["declared"] = r.File
		}

		out, err := json.MarshalIndent(resolved, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))

		if !r.Declares() {
			fmt.Fprintf(os.Stderr, "\nNo %s at or above %s. A transcript can still be fetched into a path\n"+
				"named on the call, and nothing declares where one belongs here.\n", repo.FileName, r.Root)
		}
		// A profile nobody has pointed at their own recordings selects
		// nothing, and the JSON says so only to whoever knows to look.
		for _, name := range r.ProfileNames() {
			if profile, _ := r.Profile(name); profile.Tag == "" {
				fmt.Fprintf(os.Stderr, "\nThe profile %q does not say which of your recordings it selects:\n"+
					"  plaud profile set %s --tag \"<the tag in your Plaud>\"\n", name, name)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
}

// declaredContext is the document this repository describes its recordings
// with, when it declares one. It is what makes a description something a
// repository settles once rather than something every call carries.
func declaredContext() (string, error) {
	r, err := repository()
	if err != nil || r.Context == "" {
		return "", err
	}
	data, err := os.ReadFile(r.Context)
	if err != nil {
		return "", fmt.Errorf("reading the context %s declares: %w", r.Rel(r.File), err)
	}
	return string(data), nil
}
