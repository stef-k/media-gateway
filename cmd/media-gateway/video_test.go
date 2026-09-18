package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestVideoDelivery proves GET/HEAD framing, source identity and header isolation.
func TestVideoDelivery(t *testing.T) {
	for _, tc := range []struct {
		variant, requested, contentRange, body string
		status                                 int
	}{
		{"preview", "invalid", "", "poster!", 200},
		{"original", "", "", "raw\x00vid", 200},
		{"original", "BYTES=01-03", "bytes 1-3/7", "aw\x00", 206},
		{"original", "bytes=-3", "bytes 4-6/7", "vid", 206},
		{"original", "bytes=7-", "bytes */7", "", 416},
	} {
		t.Run(tc.variant+tc.requested, func(t *testing.T) {
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/api/assets/"+testAsset {
					metadata(w, "/external/photos/website/source.mp4", "VIDEO")
					return
				}
				if r.Method != "GET" || r.URL.RequestURI() != representationTarget(testAsset, tc.variant) {
					t.Error("fallback or wrong method")
				}
				expected := ""
				if tc.status == 206 {
					expected = "bytes=1-3"
					if tc.requested == "bytes=-3" {
						expected = tc.requested
					}
				}
				if tc.status == 416 {
					expected = "bytes=7-"
				}
				if r.Header.Get("Range") != expected {
					t.Error("incorrect canonical Range")
				}
				for _, name := range []string{"If-Range", "If-None-Match", "If-Modified-Since", "If-Match", "Authorization", "Cookie", "X-Caller"} {
					if r.Header.Get(name) != "" {
						t.Error("caller header forwarded")
					}
				}
				kind := "video/mp4"
				if tc.variant == "preview" {
					kind = "image/webp"
				}
				w.Header().Set("Content-Type", kind)
				w.Header().Set("Content-Length", strconv.Itoa(len(tc.body)))
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				for _, name := range []string{"ETag", "Last-Modified", "Set-Cookie", "Content-Disposition"} {
					w.Header().Set(name, "private-marker")
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}))
			defer provider.Close()
			gateway := gatewayFor(t, provider, io.Discard, time.Second)
			for _, method := range []string{"GET", "HEAD"} {
				req, _ := http.NewRequest(method, gateway.URL+"/media/"+testAsset+"/"+tc.variant, nil)
				if tc.requested != "" {
					req.Header.Set("Range", tc.requested)
				}
				for _, name := range []string{"If-Range", "If-None-Match", "If-Modified-Since", "If-Match", "Authorization", "Cookie", "X-Caller"} {
					req.Header.Set(name, "private-marker")
				}
				resp, err := gateway.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				expected := tc.body
				if method == "HEAD" {
					expected = ""
				}
				if err != nil || resp.StatusCode != tc.status || string(body) != expected || resp.ContentLength != int64(len(tc.body)) || resp.Header.Get("Content-Range") != tc.contentRange {
					t.Fatalf("response: %d %q %v", resp.StatusCode, body, err)
				}
				ranges := ""
				if tc.variant == "original" {
					ranges = "bytes"
				}
				if resp.Header.Get("Accept-Ranges") != ranges || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("unsafe headers")
				}
				for name := range resp.Header {
					switch name {
					case "Date", "Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Cache-Control", "X-Content-Type-Options":
					default:
						t.Errorf("unreviewed header %s", name)
					}
				}
			}
			if calls.Load() != 4 {
				t.Fatal("extra representation probes")
			}
		})
	}
}

// TestVideoDenialOrdering proves range errors cannot precede current authorization.
func TestVideoDenialOrdering(t *testing.T) {
	for _, state := range []string{"eligible", "private", "outside", "near", "trashed", "offline"} {
		t.Run(state, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/assets/"+testAsset {
					t.Error("denied representation opened")
				}
				path := "/external/photos/website/a.mp4"
				switch state {
				case "private":
					path = "/external/photos/private/a.mp4"
				case "outside":
					path = "/outside/website/a.mp4"
				case "near":
					path = "/external/photos/website-old/a.mp4"
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"id": testAsset, "originalPath": path, "type": "VIDEO", "isTrashed": state == "trashed", "isOffline": state == "offline"})
			}))
			defer provider.Close()
			gateway := gatewayFor(t, provider, io.Discard, time.Second)
			if state != "eligible" {
				for _, variant := range []string{"preview", "original"} {
					req, _ := http.NewRequest("GET", gateway.URL+"/media/"+testAsset+"/"+variant, nil)
					req.Header.Set("Range", "bytes=0-1")
					resp, err := gateway.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					resp.Body.Close()
					if resp.StatusCode != 404 {
						t.Fatal("denied video fetched representation")
					}
				}
			}
			for _, ranges := range [][]string{{"bytes=0-1", "bytes=2-3"}, {"bytes=1-0"}, {"bytes=-0"}, {"bytes=0-1,2-3"}, {"bytes=9223372036854775808-"}} {
				req, _ := http.NewRequest("GET", gateway.URL+"/media/"+testAsset+"/original", nil)
				req.Header["Range"] = ranges
				resp, err := gateway.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				want, text := 404, "not found\n"
				if state == "eligible" {
					want, text = 400, "invalid range\n"
				}
				if resp.StatusCode != want || string(body) != text {
					t.Fatalf("ordering: %d %q", resp.StatusCode, body)
				}
			}
		})
	}
}

// TestVideoOriginalFailures maps provider failures before committing public bytes.
func TestVideoOriginalFailures(t *testing.T) {
	for _, status := range []int{200, 302, 401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/assets/"+testAsset {
					metadata(w, "/external/photos/website/a.mp4", "VIDEO")
					return
				}
				if r.URL.RequestURI() != representationTarget(testAsset, "original") {
					t.Error("playback fallback")
				}
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("Content-Length", "7")
				w.Header().Set("Location", "/video/playback")
				w.WriteHeader(status)
				io.WriteString(w, "private")
			}))
			defer provider.Close()
			gateway := gatewayFor(t, provider, io.Discard, time.Second)
			req, _ := http.NewRequest("GET", gateway.URL+"/media/"+testAsset+"/original", nil)
			req.Header.Set("Range", "bytes=0-2")
			resp, err := gateway.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			want, text := 502, "media unavailable\n"
			if status == 404 {
				want, text = 404, "not found\n"
			}
			if resp.StatusCode != want || string(body) != text {
				t.Fatalf("failure: %d %q", resp.StatusCode, body)
			}
		})
	}
}
