package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestShutdown proves active requests drain and a stalled request is force-closed.
func TestShutdown(t *testing.T) {
	for _, drain := range []bool{true, false} {
		t.Run(map[bool]string{true: "drain", false: "deadline"}[drain], func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			defer close(release)
			server := newServer(http.NotFoundHandler())
			server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(entered)
				select {
				case <-release:
				case <-r.Context().Done():
				}
				w.WriteHeader(http.StatusNoContent)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			stopped := make(chan error, 1)
			timeout := 50 * time.Millisecond
			if drain {
				timeout = 2 * time.Second
			}
			go func() {
				stopped <- serveListener(ctx, server, listener, slog.New(slog.NewJSONHandler(io.Discard, nil)), timeout)
			}()
			result := make(chan error, 1)
			client := &http.Client{Timeout: 3 * time.Second}
			defer client.CloseIdleConnections()
			go func() {
				response, err := client.Get("http://" + listener.Addr().String())
				if err == nil {
					response.Body.Close()
				}
				result <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("request did not enter handler")
			}
			cancel()
			if drain {
				select {
				case err := <-stopped:
					t.Fatalf("shutdown returned before drain: %v", err)
				case <-time.After(20 * time.Millisecond):
				}
				release <- struct{}{}
			}
			select {
			case err := <-stopped:
				if (err == nil) != drain {
					t.Fatalf("shutdown error: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown did not complete")
			}
			if err := <-result; drain && err != nil {
				t.Fatalf("drained request failed: %v", err)
			}
		})
	}
}
