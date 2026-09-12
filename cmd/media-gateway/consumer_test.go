package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// candidateFixture includes forbidden provider fields to detect accidental passthrough.
func candidateFixture(id, path, media string) map[string]any {
	return map[string]any{"id": id, "originalPath": path, "type": media,
		"width": 640, "height": nil, "fileCreatedAt": "2026-09-13T10:00:00Z", "localDateTime": "2026-09-13T12:00:00Z",
		"originalFileName": "filename-marker", "exifInfo": map[string]any{"GPS": "gps-marker"}, "url": "http://provider-marker", "apiKey": testKey}
}

// candidateResponse supplies the real adapter's required bounded search envelope.
func candidateResponse(w http.ResponseWriter, items []map[string]any, cursor any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"assets": map[string]any{"items": items, "count": len(items), "nextCursor": cursor}})
}

// TestConsumerBrowse proves filtering, a safe field allowlist and private-page continuation.
func TestConsumerBrowse(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "POST" || r.URL.RequestURI() != "/api/search/metadata" || r.Header.Get("x-api-key") != testKey {
			t.Error("unexpected provider request")
		}
		var query map[string]any
		if json.NewDecoder(r.Body).Decode(&query) != nil {
			t.Error("invalid query")
		}
		if query["size"] != float64(25) || query["withExif"] != false {
			t.Error("wrong search bounds")
		}
		paths := []string{"/external/photos/website/photo.jpg", "/external/photos/private/private-marker", "/outside/website/outside-marker", "/external/photos/website-old/near-marker", "/external/photos/website/../malformed-marker"}
		if query["cursor"] == "next-page" {
			paths = paths[1:]
		}
		items := make([]map[string]any, 0, len(paths))
		for i, path := range paths {
			items = append(items, candidateFixture(strings.Replace(testAsset, "12345678", "2234567"+string(rune('0'+i)), 1), path, "IMAGE"))
		}
		candidateResponse(w, items, "next-page")
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	for _, suffix := range []string{"", "?cursor=next-page"} {
		resp, err := gateway.Client().Get(gateway.URL + "/internal/assets" + suffix)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d: %s", resp.StatusCode, body)
		}
		assertPublicHeaders(t, resp)
		var page struct {
			Assets []map[string]any `json:"assets"`
			Next   string           `json:"next_cursor"`
		}
		if json.Unmarshal(body, &page) != nil || page.Next != "next-page" {
			t.Fatalf("bad page %s", body)
		}
		want := 1
		if suffix != "" {
			want = 0
		}
		if len(page.Assets) != want || !strings.Contains(string(body), "\"assets\":[") {
			t.Fatalf("bad assets %s", body)
		}
		for _, asset := range page.Assets {
			if len(asset) != 6 || asset["width"] != float64(640) || asset["height"] != nil || asset["preview_path"] != "/media/"+asset["id"].(string)+"/preview" {
				t.Fatalf("bad fields: %v", asset)
			}
		}
		for _, marker := range []string{"originalPath", "filename", "exif", "GPS", "gps-marker", testKey, "provider-marker", "private-marker", "outside-marker", "near-marker", "malformed-marker", "/external/"} {
			if strings.Contains(string(body), marker) {
				t.Errorf("leaked %s", marker)
			}
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d", calls.Load())
	}
}

// TestConsumerDetail proves known private IDs have the same fixed denial as missing IDs.
func TestConsumerDetail(t *testing.T) {
	for _, tc := range []struct {
		name, path, media string
		missing           bool
		status            int
	}{
		{"eligible", "/external/photos/website/a.jpg", "IMAGE", false, 200},
		{"private", "/external/photos/private/a.jpg", "IMAGE", false, 404},
		{"malformed", "/external/photos/website/../a.jpg", "IMAGE", false, 404},
		{"video", "/external/photos/website/a.jpg", "VIDEO", false, 404},
		{"missing", "", "", true, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var query struct {
					Filter struct {
						ID struct {
							Eq string `json:"eq"`
						} `json:"id"`
					} `json:"filter"`
					Size int `json:"size"`
				}
				if json.NewDecoder(r.Body).Decode(&query) != nil || query.Filter.ID.Eq != testAsset || query.Size != 1 {
					t.Error("not exact lookup")
				}
				items := []map[string]any{}
				if !tc.missing {
					items = append(items, candidateFixture(testAsset, tc.path, tc.media))
				}
				candidateResponse(w, items, nil)
			}))
			defer provider.Close()
			gateway := gatewayFor(t, provider, io.Discard, time.Second)
			resp, err := gateway.Client().Get(gateway.URL + "/internal/assets/" + testAsset)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tc.status {
				t.Fatalf("status %d body %s", resp.StatusCode, body)
			}
			if tc.status == 404 && string(body) != "not found\n" {
				t.Fatalf("denial %s", body)
			}
			if tc.status == 200 {
				var asset map[string]any
				if json.Unmarshal(body, &asset) != nil || len(asset) != 6 || asset["id"] != testAsset || asset["preview_path"] != testRoute {
					t.Fatalf("detail %s", body)
				}
			}
		})
	}
}
