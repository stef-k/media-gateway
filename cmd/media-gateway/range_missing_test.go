package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestVideoRangeMissing disambiguates Immich's range 404 at the public boundary.
func TestVideoRangeMissing(t *testing.T) {
	for _, tc := range []struct {
		name, requested, headers string
		status, want             int
	}{
		{"unsatisfiable", "BYTES=010-", "", 200, 416},
		{"explicit unsatisfiable", "bytes=10-20", "", 200, 416},
		{"satisfiable", "bytes=0-2", "", 200, 502},
		{"suffix satisfiable", "bytes=-20", "", 200, 502},
		{"missing", "bytes=10-", "", 404, 404},
		{"unauthorized", "bytes=10-", "", 401, 502},
		{"forbidden", "bytes=10-", "", 403, 502},
		{"failure", "bytes=10-", "", 500, 502},
		{"redirect", "bytes=10-", "Location: /video/playback\r\n", 302, 502},
		{"partial", "bytes=10-", "Content-Range: bytes 0-9/10\r\n", 206, 502},
		{"type", "bytes=10-", "Content-Type: image/jpeg\r\n", 200, 502},
		{"length", "bytes=10-", "Content-Length: 10\r\n", 200, 502},
		{"encoding", "bytes=10-", "Content-Encoding: gzip\r\n", 200, 502},
		{"transfer", "bytes=10-", "Transfer-Encoding: chunked\r\n", 200, 502},
		{"range", "bytes=10-", "Content-Range: bytes 0-9/10\r\n", 200, 502},
		{"transport", "bytes=10-", "", 0, 502},
	} {
		for _, method := range []string{"GET", "HEAD"} {
			t.Run(tc.name+method, func(t *testing.T) {
				var calls atomic.Int32
				closed := make(chan struct{})
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/assets/"+testAsset {
						metadata(w, "/external/photos/website/a.mp4", "VIDEO")
						return
					}
					n := calls.Add(1)
					if r.Method != "GET" || r.URL.RequestURI() != representationTarget(testAsset, "original") || r.Header.Get("X-Api-Key") != testKey {
						t.Error("incorrect fixed original request")
					}
					for _, name := range []string{"If-Range", "If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "Authorization", "Cookie", "X-Caller"} {
						if r.Header.Get(name) != "" {
							t.Error("caller header forwarded")
						}
					}
					if n == 1 {
						if r.Header.Get("Range") == "" || r.Header.Get("Range") == "BYTES=010-" {
							t.Error("missing canonical range")
						}
						http.Error(w, "private provider error", 404)
						return
					}
					if n != 2 || len(r.Header.Values("Range")) != 0 {
						t.Error("extra request or ranged probe")
					}
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					defer close(closed)
					if tc.status == 0 {
						return
					}
					fmt.Fprintf(rw, "HTTP/1.1 %d test\r\nContent-Type: video/mp4\r\nContent-Length: 10\r\n%s\r\n", tc.status, tc.headers)
					rw.Flush()
					// Withhold every body byte: completion must close the probe after headers.
					conn.SetReadDeadline(time.Now().Add(2 * time.Second))
					var b [1]byte
					if _, err := rw.Read(b[:]); err != io.EOF {
						t.Errorf("probe not closed promptly: %v", err)
					}
				}))
				defer provider.Close()
				gateway := gatewayFor(t, provider, io.Discard, time.Second)
				req, _ := http.NewRequest(method, gateway.URL+"/media/"+testAsset+"/original", nil)
				req.Header.Set("Range", tc.requested)
				req.Header.Set("X-Api-Key", "caller-key")
				for _, name := range []string{"If-Range", "If-None-Match", "If-Modified-Since", "If-Match", "If-Unmodified-Since", "Authorization", "Cookie", "X-Caller"} {
					req.Header.Set(name, "private caller")
				}
				resp, err := gateway.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				wantBody := "media unavailable\n"
				if tc.want == 404 {
					wantBody = "not found\n"
				}
				if tc.want == 416 || method == "HEAD" {
					wantBody = ""
				}
				if err != nil || resp.StatusCode != tc.want || string(body) != wantBody {
					t.Fatalf("response: %d %q %v", resp.StatusCode, body, err)
				}
				if tc.want == 416 && (resp.Header.Get("Content-Range") != "bytes */10" || resp.Header.Get("Content-Length") != "0" || resp.Header.Get("Accept-Ranges") != "bytes" || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff") {
					t.Fatal("incorrect 416 framing")
				}
				if calls.Load() != 2 {
					t.Fatalf("original calls: %d", calls.Load())
				}
				select {
				case <-closed:
				case <-time.After(3 * time.Second):
					t.Fatal("probe connection retained")
				}
			})
		}
	}
}
