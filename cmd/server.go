// Command brief: "server" subcommand definitions.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/toucham/morning-brief/internal/server"
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
// server.
func newServerRunCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:     "start",
		Aliases: []string{"run"},
		Short:   "Run the morning brief server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServer(cmd.Context(), addr, cmd.ErrOrStderr())
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "listen address (overrides BIND_ADDR/PORT env vars; default 127.0.0.1:8080)")
	return cmd
}

// runServer initializes the server and runs it until ctx is cancelled.
func runServer(ctx context.Context, addr string, logOut io.Writer) error {
	log := slog.New(slog.NewTextHandler(logOut, nil))
	srv := server.New(&server.Config{Addr: resolveServerAddr(addr)}, log)

	if err := srv.ListenAndServe(ctx); err != nil {
		return fmt.Errorf("server start: %w", err)
	}
	return nil
}

// resolveServerAddr picks the listen address: the --addr flag if set, else
// BIND_ADDR, else PORT (bound on all interfaces), else a local default.
func resolveServerAddr(flagAddr string) string {
	if flagAddr != "" {
		return flagAddr
	}
	if v := os.Getenv("BIND_ADDR"); v != "" {
		return v
	}
	if v := os.Getenv("PORT"); v != "" {
		return ":" + v
	}
	return "127.0.0.1:8080"
}
