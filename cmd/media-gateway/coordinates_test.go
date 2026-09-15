package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestConsumerCoordinates exercises browse/detail through the real adapter and policy.
func TestConsumerCoordinates(t *testing.T) {
	for _, tc := range []struct {
		name, exif string
		lat, lon   any
		invalid    bool
	}{
		{"valid", `{"latitude":25.0584,"longitude":121.635,"camera":"exif-marker"}`, 25.0584, 121.635, false},
		{"zero", `{"latitude":0,"longitude":0}`, float64(0), float64(0), false},
		{"minimum", `{"latitude":-90,"longitude":-180}`, float64(-90), float64(-180), false},
		{"maximum", `{"latitude":90,"longitude":180}`, float64(90), float64(180), false},
		{"absent exif", ``, nil, nil, false},
		{"null exif", `null`, nil, nil, false},
		{"absent pair", `{}`, nil, nil, false},
		{"null pair", `{"latitude":null,"longitude":null}`, nil, nil, false},
		{"latitude null only", `{"latitude":null}`, nil, nil, true},
		{"longitude null only", `{"longitude":null}`, nil, nil, true},
		{"latitude only", `{"latitude":25}`, nil, nil, true},
		{"longitude only", `{"longitude":121}`, nil, nil, true},
		{"latitude null", `{"latitude":null,"longitude":121}`, nil, nil, true},
		{"longitude null", `{"latitude":25,"longitude":null}`, nil, nil, true},
		{"string", `{"latitude":"exif-marker","longitude":121}`, nil, nil, true},
		{"boolean", `{"latitude":25,"longitude":true}`, nil, nil, true},
		{"array", `{"latitude":[],"longitude":121}`, nil, nil, true},
		{"latitude low", `{"latitude":-90.01,"longitude":0}`, nil, nil, true},
		{"latitude high", `{"latitude":90.01,"longitude":0}`, nil, nil, true},
		{"longitude low", `{"latitude":0,"longitude":-180.01}`, nil, nil, true},
		{"longitude high", `{"latitude":0,"longitude":180.01}`, nil, nil, true},
		{"overflow", `{"latitude":1e999,"longitude":0}`, nil, nil, true},
		{"NaN string", `{"latitude":"NaN","longitude":0}`, nil, nil, true},
		{"infinity string", `{"latitude":0,"longitude":"Infinity"}`, nil, nil, true},
		{"wrong exif shape", `"exif-marker"`, nil, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkCoordinateResponse(t, tc.exif, tc.invalid, tc.lat, tc.lon)
		})
	}
}

// checkCoordinateResponse asserts the complete fixed search and consumer allowlist.
func checkCoordinateResponse(t *testing.T, exif string, invalid bool, lat, lon any) {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query map[string]any
		if json.NewDecoder(r.Body).Decode(&query) != nil {
			t.Error("invalid request")
		}
		filter := map[string]any{"type": map[string]any{"in": []any{"IMAGE", "VIDEO"}}, "isOffline": map[string]any{"eq": false}, "trashedAt": map[string]any{"eq": nil}}
		filter["originalPath"] = map[string]any{"startsWith": "/external/photos/website/"}
		size := float64(25)
		if query["size"] == float64(1) {
			delete(filter, "originalPath")
			filter["id"] = map[string]any{"eq": testAsset}
			size = 1
		}
		want := map[string]any{"filter": filter, "orderBy": map[string]any{"field": "fileCreatedAt", "direction": "desc"}, "size": size, "withExif": true, "withPeople": false, "withStacked": false}
		if !reflect.DeepEqual(query, want) {
			t.Errorf("unexpected request: %v", query)
		}
		item := candidateFixture(testAsset, "/external/photos/website/a.jpg", "IMAGE")
		delete(item, "exifInfo")
		if exif != "" {
			item["exifInfo"] = json.RawMessage(exif)
		}
		candidateResponse(w, []map[string]any{item}, nil)
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
		assertPublicHeaders(t, resp)
		if invalid {
			if resp.StatusCode != 502 || string(body) != "media unavailable\n" {
				t.Fatalf("invalid coordinates: %d %s", resp.StatusCode, body)
			}
			var entry map[string]any
			if json.Unmarshal(logs.Bytes(), &entry) != nil || len(entry) != 4 || entry["msg"] != "provider search failed" || entry["outcome"] != "immich: invalid metadata response" {
				t.Fatal("unexpected failure diagnostic")
			}
		} else {
			if logs.Len() != 0 {
				t.Fatal("successful coordinates were logged")
			}
			var asset map[string]any
			if resp.StatusCode != 200 || json.Unmarshal(body, &asset) != nil {
				t.Fatalf("response: %d %s", resp.StatusCode, body)
			}
			if route == "/internal/assets?root=images&collection=website" {
				asset = asset["assets"].([]any)[0].(map[string]any)
			}
			want := map[string]any{"id": testAsset, "media_type": "image", "root": "images", "collection_path": "website", "filename": "a.jpg", "duration_ms": nil, "original_path": "/media/" + testAsset + "/original", "width": float64(640), "height": nil, "file_created_at": "2026-09-13T10:00:00Z", "local_date_time": "2026-09-13T12:00:00Z", "preview_path": testRoute}
			want["latitude"], want["longitude"] = lat, lon
			if !reflect.DeepEqual(asset, want) {
				t.Fatalf("allowlist mismatch: %v", asset)
			}
		}
		for _, marker := range []string{"exif-marker", "filename-marker", "provider-marker", testKey, "/external/", "NaN", "Infinity"} {
			if strings.Contains(string(body)+logs.String(), marker) {
				t.Errorf("leaked %s", marker)
			}
		}
	}
}

// TestCoordinatesDeniedCandidates retains private and unavailable denial/cursor semantics.
func TestCoordinatesDeniedCandidates(t *testing.T) {
	for _, state := range []string{"private", "outside", "near", "malformed", "trashed", "offline", "missing"} {
		t.Run(state, func(t *testing.T) {
			item := candidateFixture(testAsset, "/external/photos/website/a.jpg", "IMAGE")
			item["exifInfo"] = map[string]any{"latitude": 25.0584, "longitude": 121.635, "camera": "exif-marker"}
			switch state {
			case "private":
				item["originalPath"] = "/external/photos/private/a.jpg"
			case "outside":
				item["originalPath"] = "/outside/website/a.jpg"
			case "near":
				item["originalPath"] = "/external/photos/website-old/a.jpg"
			case "malformed":
				item["originalPath"] = "/external/photos/website/../a.jpg"
			case "trashed", "offline":
				item[map[string]string{"trashed": "isTrashed", "offline": "isOffline"}[state]] = true
				// Unavailable records are omitted before even malformed GPS is consumed.
				item["exifInfo"] = map[string]any{"latitude": "exif-marker"}
			}
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				items := []map[string]any{item}
				if state == "missing" {
					items = []map[string]any{}
				}
				candidateResponse(w, items, nil)
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayFor(t, provider, &logs, time.Second)
			for _, route := range []string{"/internal/assets?root=images&collection=website", "/internal/assets/" + testAsset} {
				resp, err := gateway.Client().Get(gateway.URL + route)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				status, want := 200, `{"assets":[],"next_cursor":null}`
				if strings.HasSuffix(route, testAsset) {
					status, want = 404, "not found\n"
				}
				if resp.StatusCode != status || string(body) != want || logs.Len() != 0 {
					t.Fatalf("denied response: %d %s logs %s", resp.StatusCode, body, &logs)
				}
			}
		})
	}
}

// TestCoordinatesMalformedJSON rejects non-finite JSON tokens and oversized EXIF.
func TestCoordinatesMalformedJSON(t *testing.T) {
	for _, payload := range []string{`NaN`, `Infinity`, `-Infinity`, `"` + strings.Repeat("x", 1<<20) + `"`} {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			item := candidateFixture(testAsset, "/external/photos/website/a.jpg", "IMAGE")
			item["exifInfo"] = map[string]any{"latitude": "LATITUDE", "longitude": 0}
			raw, err := json.Marshal(item)
			if err != nil {
				t.Error(err)
			}
			raw = bytes.Replace(raw, []byte(`"LATITUDE"`), []byte(payload), 1)
			fmt.Fprintf(w, `{"assets":{"items":[%s],"count":1,"nextCursor":null}}`, raw)
		}))
		gateway := gatewayFor(t, provider, io.Discard, time.Second)
		resp, err := gateway.Client().Get(gateway.URL + "/internal/assets?root=images&collection=website")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		provider.Close()
		if resp.StatusCode != 502 || string(body) != "media unavailable\n" {
			t.Fatal("malformed or oversized provider JSON accepted")
		}
	}
}

// TestCoordinateMixedPage keeps active coordinates and pagination across omitted siblings.
func TestCoordinateMixedPage(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query map[string]any
		if json.NewDecoder(r.Body).Decode(&query) != nil || query["withExif"] != true {
			t.Error("lost configured search or cursor")
		}
		items := make([]map[string]any, 4)
		for i := range items {
			items[i] = candidateFixture(fmt.Sprintf("%08x-1234-4234-8234-123456789abc", i), "/external/photos/website/a.jpg", "IMAGE")
			items[i]["exifInfo"] = map[string]any{"latitude": float64(i), "longitude": float64(i)}
		}
		items[0]["isTrashed"] = true
		items[1]["originalPath"] = "/external/photos/private/a.jpg"
		candidateResponse(w, items, nil)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	resp, err := gateway.Client().Get(gateway.URL + "/internal/assets?root=images&collection=website")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var page struct {
		Assets []map[string]any
		Next   string `json:"next_cursor"`
	}
	if json.NewDecoder(resp.Body).Decode(&page) != nil || resp.StatusCode != 200 || len(page.Assets) != 2 || page.Next != "" {
		t.Fatal("mixed page lost active items or cursor")
	}
	for i, asset := range page.Assets {
		if asset["latitude"] != float64(i+2) || asset["longitude"] != float64(i+2) {
			t.Fatal("coordinates mixed across candidates")
		}
	}
}
