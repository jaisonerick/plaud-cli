package cmd

import (
	"fmt"
	"net/http"
	"os"

	"github.com/jaisonerick/plaud-cli/internal/api"
	"github.com/jaisonerick/plaud-cli/internal/config"
	"github.com/spf13/cobra"
)

var Version = "dev"

var (
	debug   bool
	jsonOut bool
	client  *api.Client
	cfg     *config.Config
)

var rootCmd = &cobra.Command{
	Use:          "plaud",
	Short:        "CLI client for Plaud.ai",
	Version:      Version,
	SilenceUsage: true,
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// The binary an update moved aside is deleted here, on the first run
		// where nothing is holding it open.
		sweepOldBinary()
		if cmd.Name() != "update" {
			CheckForUpdate()
		}
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		client = &api.Client{
			BaseURL:  cfg.BaseURLOrDefault(),
			Token:    cfg.AccessToken,
			DeviceID: cfg.EnsureDeviceID(),
			Debug:    debug,
			HTTP:     &http.Client{},
			Session:  cfg.Session,
			// A session renewed mid-command is worth writing down there and
			// then: the cookie that was refused is gone either way, and a
			// process that exits without saving the new one leaves the next
			// one to start from the expired one it just replaced.
			OnSession: func(s *api.Session) {
				cfg.Session = s
				if err := cfg.Save(); err != nil {
					fmt.Fprintf(os.Stderr, "warning: the session was renewed but could not be saved: %v\n", err)
				}
			},
		}

		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Shell completion is a terminal's business, and this runs without one
	// almost always. Hiding it keeps the list to what a caller can act on.
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "dump raw JSON to stderr")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")
}
