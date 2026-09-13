// Command brief: "server" subcommand definitions.
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newServerCmd builds the "server" command group, which manages the local
// briefing server.
func newServerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Manage the local morning brief server",
	}
	cmd.AddCommand(newServerRunCmd())
	return cmd
}

// newServerRunCmd builds "brief server start", which runs the briefing
// server. TODO: wire internal/server's HTTP handler, listener, and graceful
// shutdown (see docs/IMPLEMENTATION.md, "Server Entrypoint").
func newServerRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "start",
		Aliases: []string{"run"},
		Short:   "Run the morning brief server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("server start: %w", errNotImplemented)
		},
	}
}
