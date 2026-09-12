package immich

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/publication"
)

// Fixtures are synthetic; no real provider identities, credentials or paths are used.
const assetID = "a1234567-abcd-4abc-8abc-123456789abc"
const testKey = "credential-marker"
const privatePath = "/external/photos/private/secret.jpg"

// response includes unrelated metadata to exercise the deliberately narrow result.
func response(id, path, media string) string {
	b, _ := json.Marshal(map[string]any{"id": id, "originalPath": path, "type": media, "exifInfo": map[string]string{"description": "body-marker"}})
	return string(b)
}

// clientFor owns both the test server and its connection pool.
func clientFor(t *testing.T, handler http.HandlerFunc, timeout time.Duration) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := New(config.Provider{BaseURL: server.URL + "/", RequestTimeout: timeout}, testKey)
	t.Cleanup(client.CloseIdleConnections)
	return client
}

// TestAssetPolicyInputs proves inspection does not grant permission, including crafted paths.
func TestAssetPolicyInputs(t *testing.T) {
	policy := config.Policy{AllowedRoots: []string{"/external/photos"}, Rules: []config.Rule{{Segment: "website", Media: []string{"image"}}}}
	for _, tc := range []struct {
		path, kind, media string
		eligible          bool
	}{
		{"/external/photos/website/a.jpg", "IMAGE", "image", true},
		{"/external/photos/website/a.mov", "VIDEO", "video", false},
		{privatePath, "IMAGE", "image", false},
		{"/external/photos/private/../website/a.jpg", "IMAGE", "image", false},
		{"/external/photos-old/website/a.jpg", "IMAGE", "image", false},
	} {
		t.Run(tc.path+tc.kind, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.RequestURI() != "/api/assets/"+assetID || r.Header.Get("x-api-key") != testKey || r.Header.Get("Authorization") != "" {
					t.Error("unexpected upstream request")
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				fmt.Fprint(w, response(assetID, tc.path, tc.kind))
			}, time.Second)
			got, err := client.Asset(context.Background(), assetID)
			if err != nil || got != (Metadata{assetID, tc.path, tc.media}) {
				t.Fatalf("unexpected mapping: %v", err)
			}
			if publication.Eligible(policy, got.OriginalPath, got.Media) != tc.eligible {
				t.Fatal("unexpected policy result")
			}
		})
	}
}

// TestAssetFailures checks bounded outcome classes and zero metadata on all denials.
func TestAssetFailures(t *testing.T) {
	good := response(assetID, privatePath, "IMAGE")
	for _, tc := range []struct {
		name              string
		status            int
		contentType, body string
		want              error
	}{
		{"missing", 404, "application/json", good, ErrMissing},
		{"unauthorized", 401, "application/json", good, ErrAuth},
		{"forbidden", 403, "application/json", good, ErrAuth},
		{"server", 500, "application/json", good, ErrProvider},
		{"unexpected", 204, "", "", ErrProvider},
		{"partial", 206, "application/json", good, ErrProvider},
		{"html", 200, "text/html", good, ErrMetadata},
		{"missing content type", 200, "", good, ErrMetadata},
		{"malformed", 200, "application/json", "body-marker", ErrMetadata},
		{"trailing JSON", 200, "application/json", good + "{}", ErrMetadata},
		{"oversize", 200, "application/json", good + strings.Repeat(" ", maxMetadata), ErrMetadata},
		{"empty path", 200, "application/json", response(assetID, "", "IMAGE"), ErrMetadata},
		{"empty id", 200, "application/json", response("", privatePath, "IMAGE"), ErrMetadata},
		{"missing fields", 200, "application/json", `{}`, ErrMetadata},
		{"null", 200, "application/json", `null`, ErrMetadata},
		{"wrong field type", 200, "application/json", `{"id":42}`, ErrMetadata},
		{"mismatch", 200, "application/json", response("b1234567-abcd-4abc-8abc-123456789abc", privatePath, "IMAGE"), ErrMetadata},
		{"empty type", 200, "application/json", response(assetID, privatePath, ""), ErrMetadata},
		{"audio", 200, "application/json", response(assetID, privatePath, "AUDIO"), ErrUnsupported},
		{"other", 200, "application/json", response(assetID, privatePath, "OTHER"), ErrUnsupported},
		{"future", 200, "application/json", response(assetID, privatePath, "FUTURE"), ErrUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header()["Content-Type"] = []string{tc.contentType}
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}, time.Second)
			got, err := client.Asset(context.Background(), assetID)
			if got != (Metadata{}) || !errors.Is(err, tc.want) {
				t.Fatalf("unexpected outcome: %v", err)
			}
			for _, secret := range []string{testKey, privatePath, "body-marker", client.endpoint} {
				if strings.Contains(fmt.Sprintf("%+v", err), secret) {
					t.Fatal("sensitive error")
				}
			}
		})
	}
}

// TestInvalidIDs proves malformed IDs never reach the configured origin.
func TestInvalidIDs(t *testing.T) {
	var calls atomic.Int32
	client := clientFor(t, func(http.ResponseWriter, *http.Request) { calls.Add(1) }, time.Second)
	for _, id := range []string{"", "../" + assetID, "https://example.com/" + assetID, assetID + "?key=secret", strings.Replace(assetID, "4abc", "7abc", 1), strings.Replace(assetID, "8abc", "0abc", 1), strings.ReplaceAll(assetID, "-", ""), " " + assetID} {
		if got, err := client.Asset(context.Background(), id); got != (Metadata{}) || err != ErrInvalidID {
			t.Fatal("invalid ID accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid ID contacted provider")
	}
}

// TestRedirects proves even same-origin redirects cannot select a different route.
func TestRedirects(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	for _, location := range []string{target.URL, "/another-route"} {
		client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/assets/"+assetID {
				forwarded.Add(1)
			}
			w.Header().Set("Location", location)
			w.WriteHeader(http.StatusFound)
		}, time.Second)
		if _, err := client.Asset(context.Background(), assetID); err != ErrProvider {
			t.Fatalf("unexpected redirect outcome: %v", err)
		}
	}
	if forwarded.Load() != 0 {
		t.Fatal("redirect followed")
	}
}

// TestRequestBounds exercises cancellation and timeout during headers and body reads.
func TestRequestBounds(t *testing.T) {
	for _, bodyStarted := range []bool{false, true} {
		for _, cancelCaller := range []bool{false, true} {
			t.Run(fmt.Sprintf("body=%t/cancel=%t", bodyStarted, cancelCaller), func(t *testing.T) {
				entered := make(chan struct{})
				client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
					if bodyStarted {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprint(w, `{"id":`)
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
				got, err := client.Asset(ctx, assetID)
				want := context.DeadlineExceeded
				if cancelCaller {
					want = context.Canceled
				}
				if got != (Metadata{}) || !errors.Is(err, want) || time.Since(started) > 2*time.Second {
					t.Fatalf("unbounded or wrong failure: %v", err)
				}
			})
		}
	}
}

// TestTransportFailure verifies URL-bearing network errors never escape the adapter.
func TestTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	client := New(config.Provider{BaseURL: server.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	if got, err := client.Asset(context.Background(), assetID); got != (Metadata{}) || err != ErrTransport {
		t.Fatalf("unexpected transport outcome: %v", err)
	}
}
