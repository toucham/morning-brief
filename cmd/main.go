// Command brief is the morning-brief CLI: it can start a local briefing
// server or run the client that connects to one.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// errNotImplemented marks a command that is wired into the Cobra tree but
// has no real behavior yet.
var errNotImplemented = errors.New("not yet implemented")

func main() {
	os.Exit(run(context.Background(), os.Args, os.Stdout, os.Stderr))
}

// run builds the brief command tree and executes it with args (excluding
// the program name), returning the process exit code: 0 on success, 1 on
// any error. ctx is cancelled on SIGINT/SIGTERM so long-running subcommands
// (the future server start) can shut down cleanly.
func run(ctx context.Context, args []string, out, errOut io.Writer) int {
	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	cmd := newRootCmd(out, errOut)
	cmd.SetArgs(args[1:])
	if err := cmd.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	return 0
}

// newRootCmd builds the "brief" command tree: "server" to manage the local
// briefing server, and "cli" to run the client against one.
func newRootCmd(out, errOut io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "brief",
		Short:         "Morning briefing server and client",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(out)
	root.SetErr(errOut)

	root.AddCommand(newServerCmd())
	root.AddCommand(newCLICmd())
	return root
}
