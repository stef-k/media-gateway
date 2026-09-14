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
		{"null absent", `{"latitude":null}`, nil, nil, false},
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
			for _, enabled := range []bool{false, true} {
				checkCoordinateResponse(t, tc.exif, enabled, tc.invalid, tc.lat, tc.lon)
			}
		})
	}
}

// checkCoordinateResponse asserts the complete fixed search and consumer allowlist.
func checkCoordinateResponse(t *testing.T, exif string, enabled, invalid bool, lat, lon any) {
	t.Helper()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var query map[string]any
		if json.NewDecoder(r.Body).Decode(&query) != nil {
			t.Error("invalid request")
		}
		filter := map[string]any{"type": map[string]any{"eq": "IMAGE"}}
		size := float64(25)
		if query["size"] == float64(1) {
			filter["id"] = map[string]any{"eq": testAsset}
			size = 1
		}
		want := map[string]any{"filter": filter, "orderBy": map[string]any{"field": "fileCreatedAt", "direction": "desc"}, "size": size, "withExif": enabled, "withPeople": false, "withStacked": false}
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
	gateway := gatewayWithCoordinates(t, provider, &logs, time.Second, enabled)
	for _, route := range []string{"/internal/assets", "/internal/assets/" + testAsset} {
		resp, err := gateway.Client().Get(gateway.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		assertPublicHeaders(t, resp)
		if enabled && invalid {
			if resp.StatusCode != 502 || string(body) != "media unavailable\n" {
				t.Fatalf("invalid coordinates: %d %s", resp.StatusCode, body)
			}
		} else {
			var asset map[string]any
			if resp.StatusCode != 200 || json.Unmarshal(body, &asset) != nil {
				t.Fatalf("response: %d %s", resp.StatusCode, body)
			}
			if route == "/internal/assets" {
				asset = asset["assets"].([]any)[0].(map[string]any)
			}
			want := map[string]any{"id": testAsset, "width": float64(640), "height": nil, "file_created_at": "2026-09-13T10:00:00Z", "local_date_time": "2026-09-13T12:00:00Z", "preview_path": testRoute}
			if enabled {
				want["latitude"], want["longitude"] = lat, lon
			}
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
				candidateResponse(w, items, "next-page")
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayWithCoordinates(t, provider, &logs, time.Second, true)
			for _, route := range []string{"/internal/assets", "/internal/assets/" + testAsset} {
				resp, err := gateway.Client().Get(gateway.URL + route)
				if err != nil {
					t.Fatal(err)
				}
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				status, want := 200, `{"assets":[],"next_cursor":"next-page"}`
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
			fmt.Fprintf(w, `{"assets":{"items":[{"exifInfo":{"latitude":%s}}],"count":1,"nextCursor":null}}`, payload)
		}))
		gateway := gatewayWithCoordinates(t, provider, io.Discard, time.Second, true)
		resp, err := gateway.Client().Get(gateway.URL + "/internal/assets")
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
