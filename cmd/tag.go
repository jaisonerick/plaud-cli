package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var tagCmd = &cobra.Command{
	Use:   "tag",
	Short: "Manage recording tags",
	Long: `List, create, and delete tags.

Examples:
  plaud tag list
  plaud tag create "Work"
  plaud tag delete "Work"`,
}

var tagListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all tags",
	RunE: func(cmd *cobra.Command, args []string) error {
		tags, err := client.ListTags(cmd.Context())
		if err != nil {
			return err
		}

		if jsonOut {
			data, _ := json.MarshalIndent(tags, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		if len(tags) == 0 {
			fmt.Println("No tags found.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tNAME")
		for _, t := range tags {
			fmt.Fprintf(w, "%s\t%s\n", t.ID, t.Name)
		}
		w.Flush()

		return nil
	},
}

var tagCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new tag",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]

		err := client.CreateTag(cmd.Context(), name)
		if err != nil {
			return fmt.Errorf("creating tag: %w", err)
		}

		fmt.Printf("Tag %q created.\n", name)
		return nil
	},
}

var tagDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a tag",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		name := args[0]

		// Resolve name to ID
		tags, err := client.ListTags(ctx)
		if err != nil {
			return err
		}

		var tagID string
		for _, t := range tags {
			if t.Name == name {
				tagID = t.ID
				break
			}
		}

		if tagID == "" {
			return fmt.Errorf("tag %q not found", name)
		}

		if err := client.DeleteTag(ctx, tagID); err != nil {
			return fmt.Errorf("deleting tag: %w", err)
		}

		fmt.Printf("Tag %q deleted.\n", name)
		return nil
	},
}

func init() {
	tagCmd.AddCommand(tagListCmd)
	tagCmd.AddCommand(tagCreateCmd)
	tagCmd.AddCommand(tagDeleteCmd)
	rootCmd.AddCommand(tagCmd)
}
