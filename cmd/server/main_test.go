package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/toucham/morning-brief/internal/api"
	"github.com/toucham/morning-brief/internal/state"
)

func TestRunManagedRejectsNonLoopback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code := run(context.Background(), []string{
		"--address", "0.0.0.0:8787",
		"--state-file", filepath.Join(t.TempDir(), "server.lock"),
	})
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error before listening)", code)
	}
}

func TestRunManagedSetButEmptyAddressIsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code := run(context.Background(), []string{
		"--address", "",
		"--state-file", filepath.Join(t.TempDir(), "server.lock"),
	})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunManagedStateConflictExitsOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	statePath := filepath.Join(dir, "server.lock")
	existing := &state.StateFile{
		PID:       9999,
		Address:   "127.0.0.1:1",
		StartedAt: time.Now().UTC(),
		Token:     strings.Repeat("7", 64),
	}
	if err := state.CreateStateFile(statePath, existing); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	code := run(ctx, []string{"--address", "127.0.0.1:0", "--state-file", statePath})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (conflict)", code)
	}
	got, err := state.ReadStateFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != 9999 || got.Token != existing.Token {
		t.Errorf("existing state was modified: %+v", got)
	}
}

func TestRunManagedPublishesOwnToken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	statePath := filepath.Join(dir, "server.lock")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- run(ctx, []string{"--address", "127.0.0.1:0", "--state-file", statePath})
	}()

	var sf *state.StateFile
	deadline := time.Now().Add(5 * time.Second)
	for {
		var err error
		sf, err = state.ReadStateFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if sf != nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("state file never published")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(sf.Token) != 64 {
		t.Fatalf("token length = %d, want 64", len(sf.Token))
	}
	for _, c := range sf.Token {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			t.Fatalf("token %q is not lowercase hex", sf.Token)
		}
	}

	req, err := http.NewRequest(http.MethodGet, "http://"+sf.Address+"/healthz", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+sf.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}
	var hr api.HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatal(err)
	}
	if hr.Token != sf.Token {
		t.Errorf("healthz token = %q, want server-generated token echoed", hr.Token)
	}

	cancel()
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d, want 0 on clean shutdown", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after ctx cancel")
	}
}
