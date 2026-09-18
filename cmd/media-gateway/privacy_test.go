package main

import (
	"bytes"
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

// TestPrivacyOffCatalogue proves hidden malformed values are ignored, with stable keys.
func TestPrivacyOffCatalogue(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		t.Run(fmt.Sprint(malformed), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var query map[string]any
				if json.NewDecoder(r.Body).Decode(&query) != nil || query["withExif"] != false {
					t.Error("privacy-off search requested EXIF")
				}
				item := candidateFixture(testAsset, "/external/photos/website/a.jpg", "IMAGE")
				item["height"], item["duration"] = 480, 23800
				item["exifInfo"] = map[string]any{"latitude": 25, "longitude": 121, "camera": "sensitive-marker"}
				if malformed {
					item["exifInfo"] = "sensitive-marker"
					item["fileCreatedAt"] = map[string]any{"invalid": "sensitive-marker"}
					item["localDateTime"] = []any{false, "sensitive-marker"}
				}
				candidateResponse(w, []map[string]any{item}, nil)
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, false)
			for _, route := range []string{"/internal/collections", "/internal/assets?root=images&collection=website", "/internal/assets/" + testAsset} {
				resp, err := gateway.Client().Get(gateway.URL + route)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				if resp.StatusCode != 200 {
					t.Fatalf("hidden metadata failed: %d %s", resp.StatusCode, body)
				}
				if strings.Contains(string(body)+logs.String(), "sensitive-marker") || logs.Len() != 0 {
					t.Fatal("source metadata leaked")
				}
				if route == "/internal/collections" {
					continue
				}
				var asset map[string]any
				if json.Unmarshal(body, &asset) != nil {
					t.Fatal("invalid JSON")
				}
				if strings.Contains(route, "?") {
					asset = asset["assets"].([]any)[0].(map[string]any)
				}
				for _, field := range []string{"file_created_at", "local_date_time", "latitude", "longitude", "original_path"} {
					value, present := asset[field]
					if !present || value != nil {
						t.Errorf("%s must be present/null", field)
					}
				}
				if len(asset) != 14 || asset["preview_path"] != testRoute || asset["width"] != float64(640) || asset["height"] != float64(480) || asset["duration_ms"] != float64(23800) || asset["filename"] != "a.jpg" {
					t.Fatalf("non-sensitive projection changed: %v", asset)
				}
			}
		})
	}
}

// TestPrivacyOffOriginals denies before metadata, range parsing or any original fetch.
func TestPrivacyOffOriginals(t *testing.T) {
	for _, media := range []string{"IMAGE", "VIDEO"} {
		t.Run(media, func(t *testing.T) {
			var originals, calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if strings.HasSuffix(r.URL.Path, "/original") {
					originals.Add(1)
					t.Error("disabled original fetched")
					return
				}
				if strings.HasSuffix(r.URL.Path, "/thumbnail") {
					w.Header().Set("Content-Type", "image/jpeg")
					w.Header().Set("Content-Length", "3")
					io.WriteString(w, "abc")
					return
				}
				metadata(w, "/external/photos/website/a.jpg", media)
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, false)
			for _, method := range []string{"GET", "HEAD"} {
				for _, requested := range []string{"", "bytes=0-1", "bytes=999-", "malformed"} {
					req, _ := http.NewRequest(method, gateway.URL+"/media/"+testAsset+"/original", nil)
					req.Header.Set("Range", requested)
					resp, err := gateway.Client().Do(req)
					if err != nil {
						t.Fatal(err)
					}
					body, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					want := "not found\n"
					if method == "HEAD" {
						want = ""
					}
					if resp.StatusCode != 404 || string(body) != want || calls.Load() != 0 || originals.Load() != 0 {
						t.Fatal("disabled original did work or changed denial")
					}
					assertPublicHeaders(t, resp)
				}
			}
			resp, err := gateway.Client().Get(gateway.URL + testRoute)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != 200 || string(body) != "abc" || originals.Load() != 0 || logs.Len() != 0 {
				t.Fatal("preview/poster changed")
			}
		})
	}
}
