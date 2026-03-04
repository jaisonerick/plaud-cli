package cmd

import (
	"fmt"
	"os"

	"github.com/jaisonerick/plaud-cli/internal/ai"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var askCmd = &cobra.Command{
	Use:   "ask <id> <question>",
	Short: "Ask a question about a recording",
	Long: `Ask a question about a recording's transcript using AI.

Requires ANTHROPIC_API_KEY environment variable.

Examples:
  plaud ask abc123 "What were the main topics discussed?"
  plaud ask abc123 "Who mentioned the budget?"
  plaud ask abc123 "Summarize what John said"`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]
		question := args[1]

		apiKey, err := ai.GetAPIKey()
		if err != nil {
			return err
		}

		// Get transcript
		detail, err := client.GetDetail(ctx, id)
		if err != nil {
			return fmt.Errorf("fetching recording details: %w", err)
		}

		url := detail.TranscriptURL()
		if url == "" {
			return fmt.Errorf("no transcript available for this recording")
		}

		fmt.Fprint(os.Stderr, "Downloading transcript... ")
		data, err := client.FetchGzipped(ctx, url)
		if err != nil {
			return fmt.Errorf("downloading transcript: %w", err)
		}

		segments, err := transcript.Parse(data)
		if err != nil {
			return fmt.Errorf("parsing transcript: %w", err)
		}
		fmt.Fprintln(os.Stderr, "done")

		text := transcript.ToText(segments)

		systemPrompt := `You are a helpful assistant that answers questions about recording transcripts.
Answer the question based only on the transcript provided. If the answer cannot be determined from the transcript, say so.
Be concise and direct. Use timestamps when referencing specific parts of the conversation.`

		userMessage := fmt.Sprintf("Here is the transcript of a recording titled %q:\n\n%s\n\nQuestion: %s", detail.Name, text, question)

		fmt.Fprintln(os.Stderr)

		if err := ai.StreamCompletion(apiKey, systemPrompt, userMessage, os.Stdout); err != nil {
			return fmt.Errorf("generating response: %w", err)
		}

		fmt.Println() // trailing newline
		return nil
	},
}

func init() {
	rootCmd.AddCommand(askCmd)
}
