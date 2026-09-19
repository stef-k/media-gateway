package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestCLI proves usage failures and shared configuration errors stay sanitized.
func TestCLI(t *testing.T) {
	for _, tc := range []struct {
		args    []string
		code    int
		message string
	}{
		{nil, 2, "invalid command line"},
		{[]string{"-unknown=private-marker"}, 2, "invalid command line"},
		{[]string{"-config", "/private-marker"}, 1, "cannot open configuration"},
		{[]string{"-version", "private-marker"}, 2, "invalid command line"},
		{[]string{"-version"}, 0, "revision="},
		{[]string{"-h"}, 0, "Usage:"},
	} {
		var out bytes.Buffer
		if got := run(context.Background(), tc.args, &out, &out); got != tc.code {
			t.Fatalf("%v: code %d", tc.args, got)
		}
		if !strings.Contains(out.String(), tc.message) || strings.Contains(out.String(), "private-marker") {
			t.Fatalf("unexpected output: %s", &out)
		}
	}
}

// writeConfig uses the committed schema and distinctive private values.
func writeConfig(t *testing.T, address string) string {
	t.Helper()
	dir := t.TempDir()
	key := filepath.Join(dir, "private-marker.key")
	if err := os.WriteFile(key, []byte("private-marker-secret"), 0600); err != nil {
		t.Fatal(err)
	}
	example, err := os.ReadFile("../../deploy/config.toml.example")
	if err != nil {
		t.Fatal(err)
	}
	contents := strings.ReplaceAll(string(example), "127.0.0.1:2290", address)
	contents = strings.ReplaceAll(contents, "/etc/media-gateway/immich.key", key)
	filename := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(filename, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

// TestStartupFailure verifies non-loopback validation and occupied-port failure.
func TestStartupFailure(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	for _, address := range []string{"0.0.0.0:2290", occupied.Addr().String()} {
		var out bytes.Buffer
		if got := run(context.Background(), []string{"-config", writeConfig(t, address)}, io.Discard, &out); got != 1 {
			t.Fatalf("exit %d", got)
		}
		if strings.Contains(out.String(), "private-marker") || strings.Contains(out.String(), "\"msg\":\"listening\"") {
			t.Fatalf("unsafe startup: %s", &out)
		}
	}
}

// TestProcess is a subprocess entry into the real signal-owning main function.
func TestProcess(t *testing.T) {
	if filename := os.Getenv("MEDIA_GATEWAY_TEST_CONFIG"); filename != "" {
		os.Args = []string{"media-gateway", "-config", filename}
		main()
	}
}

// TestSignals exercises real process startup, quiet denials, and both stop signals.
func TestSignals(t *testing.T) {
	for _, signal := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(signal.String(), func(t *testing.T) {
			reservation, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			address := reservation.Addr().String()
			reservation.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProcess$")
			cmd.Env = append(os.Environ(), "MEDIA_GATEWAY_TEST_CONFIG="+writeConfig(t, address))
			var output bytes.Buffer
			cmd.Stdout, cmd.Stderr = &output, &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			client := &http.Client{Timeout: time.Second}
			ready := false
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				response, err := client.Get("http://" + address + "/")
				if err == nil {
					response.Body.Close()
					ready = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatal("listener did not start")
			}
			for _, path := range []string{"/", "/health", "/media/known-private/preview", "/catalog/search", "/../private-marker", "/%2e%2e/private-marker", "//private-marker?url=http://private-marker"} {
				req, err := http.NewRequest(http.MethodGet, "http://"+address+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "private-marker")
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || response.StatusCode != 404 || string(body) != "not found\n" {
					t.Fatalf("unexpected denial: %d %q %v", response.StatusCode, body, err)
				}
			}
			client.CloseIdleConnections()
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err != nil {
				t.Fatalf("exit: %v; %s", err, &output)
			}
			logs := output.String()
			for _, event := range []string{"starting", "configuration loaded", "listening", "stopping", "stopped"} {
				if !strings.Contains(logs, fmt.Sprintf("\"msg\":%q", event)) {
					t.Fatalf("missing %s: %s", event, logs)
				}
			}
			if strings.Contains(logs, "private-marker") || len(strings.Split(strings.TrimSpace(logs), "\n")) != 5 {
				t.Fatalf("unexpected logs: %s", logs)
			}
		})
	}
}
