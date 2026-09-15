package immich

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestOriginalBodyLifetime proves provider timeout bounds headers, not valid bodies.
func TestOriginalBodyLifetime(t *testing.T) {
	const timeout = 100 * time.Millisecond
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "7")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		select {
		case <-time.After(3 * timeout):
			io.WriteString(w, "source!")
		case <-r.Context().Done():
		}
	}, timeout)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	original, err := client.Original(ctx, assetID)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Body.Close()
	body, err := io.ReadAll(original.Body)
	if err != nil || string(body) != "source!" {
		t.Fatalf("body inherited header timeout: %q %v", body, err)
	}
}

// TestOriginalCancellation proves header timeouts and caller cancellation stop upstream work.
func TestOriginalCancellation(t *testing.T) {
	for _, phase := range []string{"headers", "partial headers", "body"} {
		t.Run(phase, func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if phase == "partial headers" {
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					io.WriteString(rw, "HTTP/1.1 200 OK\r\nContent-Type: image/jpeg\r\n")
					rw.Flush()
					close(entered)
					// Hijacking transfers connection ownership; EOF proves client close.
					var b [1]byte
					conn.Read(b[:])
					close(stopped)
					return
				}
				if phase == "body" {
					w.Header().Set("Content-Type", "image/jpeg")
					w.Header().Set("Content-Length", "7")
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
				}
				close(entered)
				<-r.Context().Done()
				close(stopped)
			}, 100*time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				original, err := client.Original(ctx, assetID)
				if err == nil {
					defer original.Body.Close()
					<-entered
					cancel()
					_, err = io.ReadAll(original.Body)
				}
				done <- err
			}()
			select {
			case err := <-done:
				if phase == "body" {
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancellation: %v", err)
					}
				} else if err == nil {
					t.Fatal("headers unbounded")
				}
			case <-time.After(time.Second):
				t.Fatal("request did not stop")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("provider not canceled")
			}
		})
	}
}
