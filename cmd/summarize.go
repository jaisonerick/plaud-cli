package cmd

import (
	"fmt"
	"os"

	"github.com/jaisonerick/plaud-cli/internal/ai"
	"github.com/jaisonerick/plaud-cli/internal/transcript"
	"github.com/spf13/cobra"
)

var (
	sumTemplate string
	sumPrompt   string
	sumOutput   string
)

// Built-in prompt templates inspired by Plaud community templates.
var templates = map[string]string{
	"meeting": `You are a meeting secretary. Analyze the transcript and produce:

## Summary
A concise overview of the meeting (2-3 paragraphs).

## Key Decisions
Bullet list of decisions made during the meeting.

## Action Items
Bullet list of action items with assignees (if mentioned).

## Discussion Points
Brief summary of each major topic discussed.`,

	"detailed": `You are an expert note-taker. Analyze the transcript and produce a detailed, well-structured summary:

## Overview
High-level summary of the recording (1 paragraph).

## Detailed Notes
Organized by topic with timestamps where relevant. Include key quotes and context.

## Key Takeaways
The most important points from this recording.

## Follow-ups
Any open questions or items that need follow-up.`,
}

var summarizeCmd = &cobra.Command{
	Use:   "summarize <id>",
	Short: "Summarize a recording using AI",
	Long: `Generate an AI-powered summary of a recording transcript using Claude.

Requires ANTHROPIC_API_KEY environment variable.

Examples:
  plaud summarize abc123                              # Default meeting template
  plaud summarize abc123 --template detailed          # Detailed summary
  plaud summarize abc123 --prompt "List all names"    # Custom prompt
  plaud summarize abc123 --output summary.md          # Save to file`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		id := args[0]

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

		// Determine system prompt
		var systemPrompt string
		if sumPrompt != "" {
			systemPrompt = sumPrompt
		} else if tmpl, ok := templates[sumTemplate]; ok {
			systemPrompt = tmpl
		} else {
			return fmt.Errorf("unknown template %q (use: meeting, detailed, or --prompt)", sumTemplate)
		}

		// Stream response
		var writer *os.File
		if sumOutput != "" {
			writer, err = os.Create(sumOutput)
			if err != nil {
				return fmt.Errorf("creating output file: %w", err)
			}
			defer writer.Close()
			fmt.Fprintf(os.Stderr, "Generating summary (writing to %s)...\n", sumOutput)
		} else {
			writer = os.Stdout
			fmt.Fprintln(os.Stderr, "Generating summary...\n")
		}

		userMessage := fmt.Sprintf("Here is the transcript of a recording titled %q:\n\n%s", detail.Name, text)

		if err := ai.StreamCompletion(apiKey, systemPrompt, userMessage, writer); err != nil {
			return fmt.Errorf("generating summary: %w", err)
		}

		fmt.Fprintln(writer) // trailing newline
		return nil
	},
}

func init() {
	summarizeCmd.Flags().StringVar(&sumTemplate, "template", "meeting", "summary template: meeting, detailed")
	summarizeCmd.Flags().StringVar(&sumPrompt, "prompt", "", "custom system prompt (overrides --template)")
	summarizeCmd.Flags().StringVar(&sumOutput, "output", "", "save output to file")
	rootCmd.AddCommand(summarizeCmd)
}
