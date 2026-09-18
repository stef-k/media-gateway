package immich

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestRangeGrammar proves canonical forwarding and rejects ambiguous caller input.
func TestRangeGrammar(t *testing.T) {
	for _, value := range []string{"bytes=0-1", "BYTES=0002-0005", "bytes=2-", "bytes=-3", "bytes=0-9223372036854775807"} {
		r, err := ParseRange([]string{value})
		if err != nil || !strings.HasPrefix(r.String(), "bytes=") {
			t.Fatalf("valid range: %v", err)
		}
	}
	for _, values := range [][]string{{""}, {"bytes=0-1", "bytes=2-3"}, {"items=0-1"}, {"bytes=0-1,2-3"}, {"bytes=2-1"}, {"bytes=-0"}, {"bytes=-"}, {"bytes=+1-2"}, {"bytes=0- 1"}, {"bytes=0-1\t"}, {"bytes=0-1\n"}, {"bytes=0-9223372036854775808"}, {"bytes=" + strings.Repeat("0", 125) + "-1"}} {
		if _, err := ParseRange(values); err != ErrRange {
			t.Fatalf("accepted invalid syntax: %v", err)
		}
	}
}

// TestVideoOriginalRanges checks exact source slices and fixed upstream requests.
func TestVideoOriginalRanges(t *testing.T) {
	for _, tc := range []struct {
		request, canonical, contentRange, body string
		status                                 int
	}{
		{"", "", "", "0123456789", 200},
		{"BYTES=00-02", "bytes=0-2", "bytes 0-2/10", "012", 206},
		{"bytes=3-5", "bytes=3-5", "bytes 3-5/10", "345", 206},
		{"bytes=8-99", "bytes=8-99", "bytes 8-9/10", "89", 206},
		{"bytes=7-", "bytes=7-", "bytes 7-9/10", "789", 206},
		{"bytes=-3", "bytes=-3", "bytes 7-9/10", "789", 206},
		{"bytes=-99", "bytes=-99", "bytes 0-9/10", "0123456789", 206},
		{"bytes=10-", "bytes=10-", "bytes */10", "", 416},
	} {
		t.Run(tc.request, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.RequestURI() != "/api/assets/"+assetID+"/original" || r.Header.Get("Range") != tc.canonical || r.Header.Get("x-api-key") != testKey {
					t.Error("incorrect fixed request")
				}
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("Content-Length", fmt.Sprint(len(tc.body)))
				if tc.contentRange != "" {
					w.Header().Set("Content-Range", tc.contentRange)
				}
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			}, time.Second)
			var values []string
			if tc.request != "" {
				values = []string{tc.request}
			}
			requested, err := ParseRange(values)
			if err != nil {
				t.Fatal(err)
			}
			source, err := client.VideoOriginal(context.Background(), assetID, requested)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(source.Body)
			source.Body.Close()
			if err != nil || string(body) != tc.body || source.Status != tc.status || source.ContentRange != tc.contentRange {
				t.Fatalf("incorrect source: %+v %q %v", source, body, err)
			}
		})
	}
}

// TestVideoRangeWire rejects malformed/contradictory provider framing before bytes.
func TestVideoRangeWire(t *testing.T) {
	for _, tc := range []struct {
		name, requested, headers string
		status                   int
		valid                    bool
	}{
		{"valid", "bytes=2-4", "Content-Range: bytes 2-4/10\r\n", 206, true},
		{"wrong start", "bytes=2-4", "Content-Range: bytes 1-3/10\r\n", 206, false},
		{"wrong length", "bytes=2-4", "Content-Range: bytes 2-3/10\r\n", 206, false},
		{"end past total", "bytes=2-4", "Content-Range: bytes 2-4/4\r\n", 206, false},
		{"zero total", "bytes=2-4", "Content-Range: bytes 2-4/0\r\n", 206, false},
		{"overflow", "bytes=2-4", "Content-Range: bytes 2-4/9223372036854775808\r\n", 206, false},
		{"unknown total", "bytes=2-4", "Content-Range: bytes 2-4/*\r\n", 206, false},
		{"missing range", "bytes=2-4", "", 206, false},
		{"duplicate range", "bytes=2-4", "Content-Range: bytes 2-4/10\r\nContent-Range: bytes 2-4/10\r\n", 206, false},
		{"duplicate length", "bytes=2-4", "Content-Range: bytes 2-4/10\r\nContent-Length: 3\r\n", 206, false},
		{"transfer", "bytes=2-4", "Content-Range: bytes 2-4/10\r\nTransfer-Encoding: identity\r\n", 206, false},
		{"encoding", "bytes=2-4", "Content-Range: bytes 2-4/10\r\nContent-Encoding: gzip\r\n", 206, false},
		{"accept ranges", "bytes=2-4", "Content-Range: bytes 2-4/10\r\nAccept-Ranges: none\r\n", 206, false},
		{"duplicate accept", "bytes=2-4", "Content-Range: bytes 2-4/10\r\nAccept-Ranges: bytes\r\nAccept-Ranges: bytes\r\n", 206, false},
		{"full fallback", "bytes=2-4", "", 200, false},
		{"unrequested partial", "", "Content-Range: bytes 2-4/10\r\n", 206, false},
		{"valid 416", "bytes=10-", "Content-Range: bytes */10\r\n", 416, true},
		{"false 416", "bytes=2-4", "Content-Range: bytes */10\r\n", 416, false},
		{"malformed 416", "bytes=10-", "Content-Range: bytes */0\r\n", 416, false},
		{"unrequested 416", "", "Content-Range: bytes */10\r\n", 416, false},
		{"redirect", "bytes=2-4", "Location: /video/playback\r\n", 302, false},
		{"auth", "bytes=2-4", "", 401, false}, {"forbidden", "bytes=2-4", "", 403, false},
		{"missing", "bytes=2-4", "", 404, false}, {"failure", "bytes=2-4", "", 500, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/assets/"+assetID+"/original" {
					t.Error("fallback")
				}
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				fmt.Fprintf(rw, "HTTP/1.1 %d test\r\nContent-Type: video/mp4\r\nContent-Length: 3\r\n%s\r\nraw", tc.status, tc.headers)
				rw.Flush()
			}, time.Second)
			var values []string
			if tc.requested != "" {
				values = []string{tc.requested}
			}
			requested, _ := ParseRange(values)
			source, err := client.VideoOriginal(context.Background(), assetID, requested)
			if (err == nil) != tc.valid {
				t.Fatalf("validation: %v", err)
			}
			if err == nil {
				source.Body.Close()
			}
		})
	}
}
