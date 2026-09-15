package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestLifecycleRevocation exercises a running gateway as provider availability changes.
// Missing/private controls prove the exact same denial headers, body and quiet logs.
func TestLifecycleRevocation(t *testing.T) {
	for _, variant := range []string{"preview", "original"} {
		t.Run(variant, func(t *testing.T) { testLifecycleRevocation(t, variant) })
	}
}

// testLifecycleRevocation applies the same active/revoked cases to both image routes.
func testLifecycleRevocation(t *testing.T, variant string) {
	cases := []struct {
		name             string
		trashed, offline any
		status           int
	}{
		{"active", false, false, 200},
		{"trashed", true, false, 404},
		{"offline", false, true, 404},
		{"both", true, true, 404},
		{"missing", false, false, 404},
		{"private", false, false, 404},
		{"null", nil, false, 502},
		{"wrong type", false, "false", 502},
	}
	var state, previews atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/assets/"+testAsset {
			previews.Add(1)
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", "7")
			io.WriteString(w, "preview")
			return
		}
		tc := cases[state.Load()]
		if tc.name == "missing" {
			w.WriteHeader(404)
			return
		}
		item := candidateFixture(testAsset, "/external/photos/website/private-path-marker.jpg", "IMAGE")
		if tc.name == "private" {
			item["originalPath"] = "/external/photos/private/private-path-marker.jpg"
		}
		item["isTrashed"], item["isOffline"] = tc.trashed, tc.offline
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(item)
	}))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayFor(t, provider, &logs, time.Second)
	for i, tc := range cases {
		state.Store(int32(i))
		for _, method := range []string{"GET", "HEAD"} {
			req, _ := http.NewRequest(method, gateway.URL+"/media/"+testAsset+"/"+variant, nil)
			resp, err := gateway.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			want, length, kind := "not found\n", "10", "text/plain; charset=utf-8"
			if tc.status == 200 {
				want, length, kind = "preview", "7", "image/jpeg"
			}
			if tc.status == 502 {
				want, length = "media unavailable\n", "18"
			}
			if method == "HEAD" {
				want = ""
			}
			if err != nil || resp.StatusCode != tc.status || string(body) != want || resp.Header.Get("Content-Length") != length || resp.Header.Get("Content-Type") != kind {
				t.Fatalf("%s %s: %d %q %v", tc.name, method, resp.StatusCode, body, err)
			}
			assertPublicHeaders(t, resp)
		}
		if previews.Load() != 2 {
			t.Fatalf("%s fetched a denied preview", tc.name)
		}
		if tc.status != 502 && logs.Len() != 0 {
			t.Fatal("routine denial logged")
		}
	}
	for _, marker := range []string{testKey, "private-path-marker", "gps-marker", "isTrashed", "isOffline", provider.URL} {
		if strings.Contains(logs.String(), marker) {
			t.Fatal("private metadata logged")
		}
	}
}

// TestConsumerLifecycle omits unavailable candidates, preserving active siblings and
// continuation; an exact unavailable ID is a quiet 404, malformed flags remain 502.
func TestConsumerLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name             string
		trashed, offline any
		invalid          bool
	}{
		{"trashed", true, false, false},
		{"offline", false, true, false},
		{"both", true, true, false},
		{"invalid", nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/search/metadata" {
					t.Error("unexpected provider fetch")
				}
				var query struct{ Filter map[string]any }
				json.NewDecoder(r.Body).Decode(&query)
				stale := candidateFixture(testAsset, "/external/photos/website/private-path-marker.jpg", "IMAGE")
				stale["isTrashed"], stale["isOffline"] = tc.trashed, tc.offline
				items := []map[string]any{stale}
				if query.Filter["id"] == nil {
					items = append(items, candidateFixture("22345678-1234-4234-8234-123456789abc", "/external/photos/website/active.jpg", "IMAGE"))
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
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				want := 200
				if strings.HasSuffix(route, testAsset) {
					want = 404
				}
				if tc.invalid {
					want = 502
				}
				if err != nil || resp.StatusCode != want {
					t.Fatalf("response %d %q %v", resp.StatusCode, body, err)
				}
				if want == 200 {
					var page consumerPage
					if json.Unmarshal(body, &page) != nil || len(page.Assets) != 1 || page.NextCursor != nil {
						t.Fatal("active sibling or cursor lost")
					}
				}
				if want == 404 && string(body) != "not found\n" {
					t.Fatal("wrong denial body")
				}
				if want == 502 && string(body) != "media unavailable\n" {
					t.Fatal("wrong failure body")
				}
				if strings.Contains(string(body), testAsset) {
					t.Fatal("stale candidate exposed")
				}
				assertPublicHeaders(t, resp)
			}
			if !tc.invalid && logs.Len() != 0 {
				t.Fatal("lifecycle denial logged")
			}
			for _, marker := range []string{testKey, "private-path-marker", "gps-marker", "isTrashed", "isOffline"} {
				if strings.Contains(logs.String(), marker) {
					t.Fatal("private metadata logged")
				}
			}
		})
	}
}
