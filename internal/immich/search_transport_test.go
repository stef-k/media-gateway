package immich

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
)

// TestSearchRedirects rejects both authorities and same-origin route changes.
func TestSearchRedirects(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	for _, location := range []string{target.URL, "/another-route"} {
		client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/search/metadata" {
				forwarded.Add(1)
			}
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusTemporaryRedirect)
		}, time.Second)
		if _, err := client.SearchCandidates(context.Background(), CandidateQuery{Limit: 1}); err != ErrProvider {
			t.Fatalf("unexpected redirect outcome: %v", err)
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

// TestSearchRequestBounds exercises cancellation and timeout at headers and body.
func TestSearchRequestBounds(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		for _, cancelCaller := range []bool{false, true} {
			t.Run(fmt.Sprintf("body=%t/cancel=%t", bodyStarted, cancelCaller), func(t *testing.T) {
				entered := make(chan struct{})
				client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
					// Consume the POST body so disconnect cancellation reaches the handler.
					_, _ = io.Copy(io.Discard, r.Body)
					if bodyStarted {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"assets":`)
						w.(http.Flusher).Flush()
					}
					close(entered)
					<-r.Context().Done()
				}, 100*time.Millisecond)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if cancelCaller {
					go func() { <-entered; cancel() }()
				}
				started := time.Now()
				got, err := client.SearchCandidates(ctx, CandidateQuery{Limit: 1})
				want := context.DeadlineExceeded
				if cancelCaller {
					want = context.Canceled
				}
				if !errors.Is(err, want) || got.Items != nil || got.NextCursor != "" || time.Since(started) > 2*time.Second {
					t.Fatalf("unbounded or wrong failure: %v", err)
				}
			})
		}
	}
}

// TestSearchTransportFailure ensures URL-bearing network failures stay sanitized.
func TestSearchTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	client := New(config.Provider{BaseURL: server.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	if got, err := client.SearchCandidates(context.Background(), CandidateQuery{Limit: 1}); err != ErrTransport || got.Items != nil || got.NextCursor != "" {
		t.Fatalf("unexpected transport outcome: %v", err)
	}
}
