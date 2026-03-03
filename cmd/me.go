package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var meCmd = &cobra.Command{
	Use:   "me",
	Short: "Show authenticated user info",
	RunE: func(cmd *cobra.Command, args []string) error {
		user, err := client.GetMe(cmd.Context())
		if err != nil {
			return err
		}

		if jsonOut {
			data, _ := json.MarshalIndent(user, "", "  ")
			fmt.Println(string(data))
			return nil
		}

		fmt.Printf("ID:      %s\n", user.ID)
		fmt.Printf("Email:   %s\n", user.Email)
		fmt.Printf("Name:    %s\n", user.Nickname)
		if user.Country != "" {
			fmt.Printf("Country: %s\n", user.Country)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(meCmd)
}
