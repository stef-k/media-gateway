package immich

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestOriginalStream proves the fixed source endpoint preserves arbitrary image bytes.
func TestOriginalStream(t *testing.T) {
	for _, kind := range []string{"image/jpeg", "image/heic", "image/x-sony-arw"} {
		t.Run(kind, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.RequestURI() != "/api/assets/"+assetID+"/original" || r.Header.Get("x-api-key") != testKey {
					t.Error("unexpected original request")
				}
				for _, h := range []string{"Range", "If-Range", "If-None-Match", "If-Modified-Since", "Authorization", "Cookie"} {
					if r.Header.Get(h) != "" {
						t.Errorf("forwarded %s", h)
					}
				}
				w.Header().Set("Content-Type", kind)
				w.Header().Set("Content-Length", "7")
				_, _ = io.WriteString(w, "raw\x00img")
			}, time.Second)
			original, err := client.Original(context.Background(), assetID)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(original.Body)
			original.Body.Close()
			if err != nil || string(body) != "raw\x00img" || original.ContentType != kind || original.Length != 7 {
				t.Fatalf("incorrect original: %q %v", body, err)
			}
		})
	}
}

// TestOriginalHeaders uses wire responses because net/http can normalize duplicate lengths.
func TestOriginalHeaders(t *testing.T) {
	for _, tc := range []struct {
		name, headers string
		status        int
		want          error
	}{
		{"jpeg", "Content-Type: image/jpeg\r\nContent-Length: 7\r\n", 200, nil},
		{"large", "Content-Type: image/heic\r\nContent-Length: 16777217\r\n", 200, nil},
		{"non image", "Content-Type: video/mp4\r\nContent-Length: 7\r\n", 200, ErrOriginal},
		{"missing type", "Content-Length: 7\r\n", 200, ErrOriginal},
		{"invalid type", "Content-Type: image/\r\nContent-Length: 7\r\n", 200, ErrOriginal},
		{"parameters", "Content-Type: image/jpeg; foo=bar\r\nContent-Length: 7\r\n", 200, ErrOriginal},
		{"duplicate type", "Content-Type: image/jpeg\r\nContent-Type: image/jpeg\r\nContent-Length: 7\r\n", 200, ErrOriginal},
		{"missing length", "Content-Type: image/jpeg\r\n", 200, ErrOriginal},
		{"zero", "Content-Type: image/jpeg\r\nContent-Length: 0\r\n", 200, ErrOriginal},
		{"negative", "Content-Type: image/jpeg\r\nContent-Length: -1\r\n", 200, ErrTransport},
		{"invalid length", "Content-Type: image/jpeg\r\nContent-Length: bad\r\n", 200, ErrTransport},
		{"overflow", "Content-Type: image/jpeg\r\nContent-Length: 9223372036854775808\r\n", 200, ErrTransport},
		{"duplicate length", "Content-Type: image/jpeg\r\nContent-Length: 7\r\nContent-Length: 7\r\n", 200, ErrTransport},
		{"different lengths", "Content-Type: image/jpeg\r\nContent-Length: 7\r\nContent-Length: 8\r\n", 200, ErrTransport},
		{"transfer", "Content-Type: image/jpeg\r\nTransfer-Encoding: chunked\r\n", 200, ErrTransport},
		{"encoding", "Content-Type: image/jpeg\r\nContent-Length: 7\r\nContent-Encoding: gzip\r\n", 200, ErrOriginal},
		{"range", "Content-Type: image/jpeg\r\nContent-Length: 7\r\nContent-Range: bytes 0-6/7\r\n", 200, ErrOriginal},
		{"missing", "Content-Length: 7\r\n", 404, ErrMissing},
		{"chunked missing", "Transfer-Encoding: chunked\r\n", 404, ErrMissing},
		{"chunked auth", "Transfer-Encoding: chunked\r\n", 401, ErrAuth},
		{"transfer identity", "Content-Type: image/jpeg\r\nContent-Length: 7\r\nTransfer-Encoding: identity\r\n", 200, ErrTransport},
		{"informational", "Content-Length: 7\r\n", 103, ErrTransport},
		{"oversized headers", "Content-Type: image/jpeg\r\nContent-Length: 7\r\nX-Large: " + strings.Repeat("x", 16<<10) + "\r\n", 200, ErrTransport},
		{"auth", "Content-Length: 7\r\n", 401, ErrAuth},
		{"forbidden", "Content-Length: 7\r\n", 403, ErrAuth},
		{"failure", "Content-Length: 7\r\n", 500, ErrProvider},
		{"redirect", "Content-Length: 7\r\nLocation: /escape\r\n", 302, ErrProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/assets/"+assetID+"/original" {
					t.Error("redirect/fallback followed")
				}
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				fmt.Fprintf(rw, "HTTP/1.1 %d test\r\n%s\r\nsource!", tc.status, tc.headers)
				rw.Flush()
			}, time.Second)
			got, err := client.Original(context.Background(), assetID)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if err == nil {
				got.Body.Close()
			} else if got != (Original{}) {
				t.Fatal("partial result")
			}
		})
	}
}

// TestOriginalTruncated returns only sanitized stream errors, never shorter success.
func TestOriginalTruncated(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "7")
		io.WriteString(w, "raw")
	}, time.Second)
	original, err := client.Original(context.Background(), assetID)
	if err != nil {
		t.Fatal(err)
	}
	defer original.Body.Close()
	_, err = io.ReadAll(original.Body)
	if !errors.Is(err, ErrTransport) || strings.Contains(err.Error(), "http") {
		t.Fatalf("unsafe stream failure: %v", err)
	}
}
