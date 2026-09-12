package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestDeliveryBounds proves cancellation and provider deadlines stop actual HTTP
// work both before public headers and during streaming, without whole-image buffering.
func TestDeliveryBounds(t *testing.T) {
	for _, phase := range []string{"metadata", "preview headers", "preview body"} {
		for _, cancelCaller := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cancel=%t", phase, cancelCaller), func(t *testing.T) {
				entered, stopped := make(chan struct{}), make(chan struct{})
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if phase != "metadata" && r.URL.Path == "/api/assets/"+testAsset {
						metadata(w, "/external/photos/website/photo.jpg", "IMAGE")
						return
					}
					if phase == "preview body" {
						w.Header().Set("Content-Type", "image/jpeg")
						w.Header().Set("Content-Length", "65536")
						_, _ = io.WriteString(w, strings.Repeat("p", 8192))
						w.(http.Flusher).Flush()
					}
					close(entered)
					<-r.Context().Done()
					close(stopped)
				}))
				defer provider.Close()
				timeout := 150 * time.Millisecond
				if cancelCaller {
					timeout = 3 * time.Second
				}
				gateway := gatewayFor(t, provider, io.Discard, timeout)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					req, _ := http.NewRequestWithContext(ctx, "GET", gateway.URL+testRoute, nil)
					resp, err := gateway.Client().Do(req)
					if err == nil {
						_, err = io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
						if !cancelCaller && phase != "preview body" && resp.StatusCode != 502 {
							err = fmt.Errorf("status %d", resp.StatusCode)
						}
					}
					done <- err
				}()
				select {
				case <-entered:
				case <-ctx.Done():
					t.Fatal("provider not entered")
				}
				if cancelCaller {
					cancel()
				}
				select {
				case err := <-done:
					if cancelCaller || phase == "preview body" {
						if err == nil {
							t.Error("incomplete stream succeeded")
						}
					} else if err != nil {
						t.Error(err)
					}
				case <-time.After(2 * time.Second):
					t.Fatal("public work did not stop")
				}
				select {
				case <-stopped:
				case <-time.After(time.Second):
					t.Fatal("provider work did not stop")
				}
			})
		}
	}
}

// TestTruncatedPreview proves a post-header failure cannot become a complete image
// or a successful shorter body; the declared public length remains authoritative.
func TestTruncatedPreview(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/assets/"+testAsset {
			metadata(w, "/external/photos/website/photo.jpg", "IMAGE")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "16384")
		_, _ = io.WriteString(w, strings.Repeat("p", 8192))
		w.(http.Flusher).Flush()
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	resp, err := gateway.Client().Get(gateway.URL + testRoute)
	if err != nil {
		return
	} // An abort before headers reach the socket is also closed.
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err == nil || len(body) >= 16384 || strings.Contains(string(body), "unavailable") {
		t.Fatalf("truncated response accepted: bytes=%d err=%v", len(body), err)
	}
}

// TestMetadataAndTransportFailures proves HTTP orchestration sanitizes failures
// before authorization and never requests a preview after failed metadata retrieval.
func TestMetadataAndTransportFailures(t *testing.T) {
	for _, status := range []int{401, 403, 500, 200, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/assets/"+testAsset {
					t.Error("preview fetched after metadata failure")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "private-provider-body")
			}))
			defer provider.Close()
			if status == 0 {
				provider.Close()
			}
			gateway := gatewayFor(t, provider, io.Discard, time.Second)
			resp, err := gateway.Client().Get(gateway.URL + testRoute)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 502 || string(body) != "media unavailable\n" {
				t.Fatalf("response %d %q %v", resp.StatusCode, body, err)
			}
		})
	}
}

// TestHeadClosesPreview verifies HEAD does not drain or wait for a complete image.
func TestHeadClosesPreview(t *testing.T) {
	stopped := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/assets/"+testAsset {
			metadata(w, "/external/photos/website/photo.jpg", "IMAGE")
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "65536")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, 3*time.Second)
	resp, err := gateway.Client().Head(gateway.URL + testRoute)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || len(body) != 0 || resp.ContentLength != 65536 {
		t.Fatalf("HEAD response: %d %q %v", resp.StatusCode, body, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HEAD kept provider stream alive")
	}
}
