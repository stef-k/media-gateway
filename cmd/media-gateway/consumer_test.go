package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// candidateFixture includes forbidden provider fields to detect accidental passthrough.
func candidateFixture(id, path, media string) map[string]any {
	return map[string]any{"id": id, "originalPath": path, "type": media, "isTrashed": false, "isOffline": false,
		"duration": nil, "width": 640, "height": nil, "fileCreatedAt": "2026-09-13T10:00:00Z", "localDateTime": "2026-09-13T12:00:00Z",
		"originalFileName": "filename-marker", "exifInfo": map[string]any{"GPS": "gps-marker"}, "url": "http://provider-marker", "apiKey": testKey}
}

// candidateResponse supplies the real adapter's required bounded search envelope.
func candidateResponse(w http.ResponseWriter, items []map[string]any, cursor any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"assets": map[string]any{"items": items, "count": len(items), "nextCursor": cursor}})
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
		{"video", "/external/photos/website/a.jpg", "VIDEO", false, 200},
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
				if json.Unmarshal(body, &asset) != nil || len(asset) != 14 || asset["id"] != testAsset {
					t.Fatalf("detail %s", body)
				}
				if tc.media == "IMAGE" && asset["original_path"] != "/media/"+testAsset+"/original" {
					t.Fatal("image original capability missing")
				}
				if tc.media == "VIDEO" && (asset["original_path"] != nil || asset["preview_path"] != nil) {
					t.Fatal("video capability exposed early")
				}
			}
		})
	}
}
