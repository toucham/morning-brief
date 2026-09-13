// Command morning-server is the local briefing server. It never handles
// state cleanup: a stopped or crashed server leaves its state file in place
// for the CLI to reconcile under the lifecycle lock.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/toucham/morning-brief/internal/server"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:]))
}

// run executes the server and returns the process exit code: 0 clean,
// 1 runtime failure (config, bind, state conflict, serve), 2 usage error.
func run(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("morning-server", flag.ContinueOnError)
	address := fs.String("address", "", "listen address (default: server.yaml address or 127.0.0.1:8787)")
	stateFile := fs.String("state-file", "", "state file path; when set, the server runs in managed mode")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cfg, err := config.LoadServerConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "morning-server:", err)
		return 1
	}

	hasExplicitAddress := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "address" {
			hasExplicitAddress = true
		}
	})
	if hasExplicitAddress && *address == "" {
		fmt.Fprintln(os.Stderr, "morning-server: --address was set but is empty")
		return 2
	}

	// Address precedence: explicit --address > server.yaml > default.
	addr := cfg.Address
	if hasExplicitAddress {
		addr = *address
	}
	if addr == "" {
		addr = "127.0.0.1:8787"
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "morning-server:", err)
		return 1
	}
	// Managed-mode setup below can still fail; release the port in that case.
	// After Serve returns the listener is already closed, so this second
	// Close is a no-op.
	defer ln.Close()

	token := ""
	if isManaged {
		if err := os.MkdirAll(filepath.Dir(*stateFile), 0o700); err != nil {
			fmt.Fprintln(os.Stderr, "morning-server:", err)
			return 1
		}
		token, err = state.GenerateToken()
		if err != nil {
			fmt.Fprintln(os.Stderr, "morning-server:", err)
			return 1
		}
		sf := &state.StateFile{
			PID:       os.Getpid(),
			Address:   ln.Addr().String(),
			StartedAt: time.Now().UTC(),
			Token:     token,
		}
		if err := state.CreateStateFile(*stateFile, sf); err != nil {
			if errors.Is(err, state.ErrExists) {
				fmt.Fprintf(os.Stderr, "morning-server: state file %s already exists; refusing to start\n", *stateFile)
			} else {
				fmt.Fprintln(os.Stderr, "morning-server:", err)
			}
			return 1
		}
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv := server.New(cfg, token, logger)

	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	if err := srv.Serve(ctx, ln); err != nil {
		fmt.Fprintln(os.Stderr, "morning-server:", err)
		return 1
	}
	return 0
}
