package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestOriginalWriteLifetime exercises actual socket writes beyond the server's
// short default and verifies finite failure when the downstream stops reading.
func TestOriginalWriteLifetime(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "stalled"}[active], func(t *testing.T) {
			done := make(chan struct{})
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				reader, writer := io.Pipe()
				defer reader.Close()
				go func() {
					defer writer.Close()
					chunk := strings.Repeat("x", 32<<10)
					for i := 0; i < 4096; i++ {
						if active {
							time.Sleep(30 * time.Millisecond)
							chunk = "x"
							if i == 8 {
								return
							}
						}
						if _, err := io.WriteString(writer, chunk); err != nil {
							return
						}
					}
				}()
				streamOriginal(w, r, reader, slog.New(slog.NewTextHandler(io.Discard, nil)), 150*time.Millisecond)
			}))
			server.Config = newServer(server.Config.Handler)
			server.Config.WriteTimeout = 50 * time.Millisecond
			server.Start()
			defer server.Close()
			if active {
				response, err := server.Client().Get(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(response.Body)
				response.Body.Close()
				if err != nil || string(body) != "xxxxxxxx" {
					t.Fatalf("active stream failed: %q %v", body, err)
				}
			} else {
				conn, err := net.Dial("tcp", server.Listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				conn.(*net.TCPConn).SetReadBuffer(1024)
				fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: localhost\r\n\r\n")
			}
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("write inactivity was unbounded")
			}
		})
	}
}

// TestOriginalShutdown releases the actual provider stream when graceful drain expires.
func TestOriginalShutdown(t *testing.T) {
	stopped := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/assets/"+testAsset {
			metadata(w, "/external/photos/website/a.mp4", "VIDEO")
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "1048576")
		io.WriteString(w, strings.Repeat("x", 32<<10))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}))
	defer provider.Close()
	fixture := gatewayFor(t, provider, io.Discard, time.Second)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- serveListener(ctx, newServer(fixture.Config.Handler), listener, slog.New(slog.NewTextHandler(io.Discard, nil)), 50*time.Millisecond)
	}()
	response, err := http.Get("http://" + listener.Addr().String() + "/media/" + testAsset + "/original")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("stalled stream unexpectedly drained")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown unbounded")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("provider stream survived shutdown")
	}
}
