package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestConsumerInputBoundary proves invalid requests cannot reach provider search.
func TestConsumerInputBoundary(t *testing.T) {
	for _, expose := range []bool{false, true} {
		name := "off"
		if expose {
			name = "on"
		}
		t.Run(name, func(t *testing.T) { testConsumerInputBoundary(t, expose) })
	}
}

// testConsumerInputBoundary applies the same security boundary in either privacy mode.
func testConsumerInputBoundary(t *testing.T, expose bool) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, expose)
	for _, route := range []string{
		"/internal/assets/", "/internal/assets/not-a-uuid", "/internal/assets/" + testAsset + "/preview",
		"/internal/assets/" + testAsset + "?limit=1", "/internal/assets/" + testAsset + "?",
		"/internal/assets/%31" + testAsset[1:], "/internal//assets", "/internal/assets/../assets", "/internal/search",
		"/assets", "/search", "/config", "/metadata", "/provider", "/health",
		"/internal/assets?limit=0", "/internal/assets?limit=101", "/internal/assets?limit=-1", "/internal/assets?limit=%2B1",
		"/internal/assets?limit=1.5", "/internal/assets?limit=999999999999999999999", "/internal/assets?limit=",
		"/internal/assets?limit=1&limit=2", "/internal/assets?cursor=", "/internal/assets?cursor=a&cursor=b",
		"/internal/assets?cursor=%00", "/internal/assets?cursor=%C2%85", "/internal/assets?cursor=%FF", "/internal/assets?cursor=%zz",
		"/internal/assets?cursor=" + strings.Repeat("a", 1025), "/internal/assets?cursor=" + strings.Repeat("a", 4097),
		"/internal/assets?filter=%7B%7D", "/internal/assets?path=/private", "/internal/assets?url=http://attacker.invalid",
		"/internal/assets?withExif=true", "/internal/assets?expose_coordinates=true",
		"/internal/assets?type=VIDEO", "/internal/assets?sort=originalPath", "/internal/assets?limit=1;cursor=a",
	} {
		resp, err := gateway.Client().Get(gateway.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 404 || string(body) != "not found\n" {
			t.Errorf("%s: %d %s", route, resp.StatusCode, body)
		}
	}
	for _, method := range []string{"HEAD", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"} {
		req, _ := http.NewRequest(method, gateway.URL+"/internal/assets?root=images&collection=website", nil)
		resp, err := gateway.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s: %d", method, resp.StatusCode)
		}
	}
	// Direct handler calls simulate remote peers without opening a non-loopback listener.
	for _, peer := range []string{"192.0.2.1:1234", "[2001:db8::1]:1234", "localhost:1234", ""} {
		req := httptest.NewRequest("GET", "/internal/assets?root=images&collection=website", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "127.0.0.1")
		req.Header.Set("Forwarded", "for=127.0.0.1")
		resp := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(resp, req)
		if resp.Code != 404 {
			t.Errorf("peer %q accepted", peer)
		}
	}
	if calls.Load() != 0 || logs.Len() != 0 {
		t.Fatalf("provider calls %d logs %s", calls.Load(), &logs)
	}
}

// TestConsumerFailures proves provider values cannot escape generic error handling.
func TestConsumerFailures(t *testing.T) {
	for _, status := range []int{401, 403, 404, 500, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "private-body-marker "+testKey+" /private/path http://provider-marker")
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayFor(t, provider, &logs, time.Second)
			for _, route := range []string{"/internal/assets?root=images&collection=website", "/internal/assets/" + testAsset} {
				logs.Reset()
				resp, err := gateway.Client().Get(gateway.URL + route)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 502 || string(body) != "media unavailable\n" {
					t.Fatalf("%d %s", resp.StatusCode, body)
				}
				if status == 404 {
					var record struct {
						Level   string
						Msg     string
						Outcome string
					}
					if json.Unmarshal(logs.Bytes(), &record) != nil || record.Level != "WARN" || record.Msg != "provider search failed" || record.Outcome != "immich: unexpected provider status" {
						t.Fatalf("missing fixed search failure diagnostic: %s", &logs)
					}
				}
				for _, marker := range []string{testKey, "private-body-marker", "/private/path", "provider-marker", provider.URL} {
					if strings.Contains(logs.String()+string(body), marker) {
						t.Errorf("leaked %s", marker)
					}
				}
			}
		})
	}
}

// TestConsumerCancellation proves disconnects and configured timeouts stop search.
func TestConsumerCancellation(t *testing.T) {
	for _, cancelCaller := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelCaller), func(t *testing.T) {
			entered, stopped := make(chan struct{}), make(chan struct{})
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
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
				req, _ := http.NewRequestWithContext(ctx, "GET", gateway.URL+"/internal/assets?root=images&collection=website", nil)
				resp, err := gateway.Client().Do(req)
				if err == nil {
					resp.Body.Close()
					if resp.StatusCode != 502 {
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
				if cancelCaller && err == nil {
					t.Error("canceled request succeeded")
				}
				if !cancelCaller && err != nil {
					t.Error(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("consumer work did not stop")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("provider work did not stop")
			}
		})
	}
}

// TestConsumerResponseBound exercises the encoded-size cap with valid provider times.
func TestConsumerResponseBound(t *testing.T) {
	for _, expose := range []bool{false, true} {
		name := "off"
		if expose {
			name = "on"
		}
		t.Run(name, func(t *testing.T) { testConsumerResponseBound(t, expose) })
	}
}

// testConsumerResponseBound applies the same security boundary in either privacy mode.
func testConsumerResponseBound(t *testing.T, expose bool) {
	var oversized atomic.Bool
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := make([]map[string]any, 100)
		for i := range items {
			items[i] = candidateFixture(fmt.Sprintf("%08x-1234-4234-8234-123456789abc", i), "/external/photos/website/a.jpg", "IMAGE")
			if oversized.Load() {
				items[i]["originalPath"] = "/external/photos/website/" + strings.Repeat("a", 5200) + ".jpg"
			}
		}
		candidateResponse(w, items, nil)
	}))
	defer provider.Close()
	gateway := gatewayWithPrivacy(t, provider, io.Discard, time.Second, expose)
	for _, large := range []bool{false, true} {
		oversized.Store(large)
		resp, err := gateway.Client().Get(gateway.URL + "/internal/assets?root=images&collection=website&limit=100")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if large {
			if resp.StatusCode != 502 || string(body) != "media unavailable\n" {
				t.Fatalf("%d: %d bytes", resp.StatusCode, len(body))
			}
		} else {
			var page consumerPage
			if resp.StatusCode != 200 || len(body) > maxConsumerJSONBytes || json.Unmarshal(body, &page) != nil || len(page.Assets) != 100 || !strings.Contains(string(body), `"next_cursor":null`) {
				t.Fatalf("bounded page: %d, %d bytes", resp.StatusCode, len(body))
			}
		}
	}
}
