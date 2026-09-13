// Command brief: "cli" subcommand definitions.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newCLICmd builds "brief cli", which connects to a briefing server. TODO:
// wire internal/client's HTTP client.
func newCLICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cli",
		Short: "Run the client against a briefing server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("cli: %w", errNotImplemented)
		},
	}
}
