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
	Use:     "plaud",
	Short:   "CLI client for Plaud.ai",
	Version: Version,
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
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "dump raw JSON to stderr")
	rootCmd.PersistentFlags().BoolVar(&jsonOut, "json", false, "output as JSON")
}
