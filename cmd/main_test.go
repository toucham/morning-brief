package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantErrSub string // substring expected in stderr; "" = don't check
	}{
		{"no args shows help", []string{"brief"}, 0, ""},
		{"help flag", []string{"brief", "--help"}, 0, ""},
		{"cli not implemented", []string{"brief", "cli"}, 1, "cli: not yet implemented"},
		{"unknown subcommand", []string{"brief", "bogus"}, 1, ""},
		{"server start listen error on invalid address", []string{"brief", "server", "start", "--addr", "999.999.999.999:99999"}, 1, "server start: serve: listen"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			got := run(context.Background(), tt.args, &out, &errOut)
			if got != tt.wantExit {
				t.Errorf("run(%v) exit = %d, want %d (stderr=%q)", tt.args, got, tt.wantExit, errOut.String())
			}
			if tt.wantErrSub != "" && !strings.Contains(errOut.String(), tt.wantErrSub) {
				t.Errorf("run(%v) stderr = %q, want substring %q", tt.args, errOut.String(), tt.wantErrSub)
			}
		})
	}
}

func TestRun_ServerStart_ShutdownOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var out, errOut bytes.Buffer
	done := make(chan int, 1)

	go func() {
		done <- run(ctx, []string{"brief", "server", "start", "--addr", "127.0.0.1:0"}, &out, &errOut)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if got != 0 {
			t.Fatalf("run() exit = %d, want 0 (stderr=%q)", got, errOut.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("run() did not return within 3s of cancellation")
	}
}
