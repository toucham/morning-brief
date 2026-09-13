package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
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
		{"server start not implemented", []string{"brief", "server", "start"}, 1, "server start: not yet implemented"},
		{"cli not implemented", []string{"brief", "cli"}, 1, "cli: not yet implemented"},
		{"unknown subcommand", []string{"brief", "bogus"}, 1, ""},
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
