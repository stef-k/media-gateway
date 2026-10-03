package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestFilenameSearch preserves projection and matches only authorized basenames.
func TestFilenameSearch(t *testing.T) {
	for _, expose := range []bool{false, true} {
		t.Run(fmt.Sprint(expose), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var query struct{ Filter map[string]any }
				if json.NewDecoder(r.Body).Decode(&query) != nil || query.Filter["originalFileName"] != nil {
					t.Error("search narrowed by provider display filename")
				}
				paths := []string{"website/20231026_215006_Été_DxO.jpg", "website/215006_Été.mp4", "website/other.jpg", "website/child/215006_Été.jpg", "private/215006_Été.jpg", "website-old/215006_Été.jpg", "../website/215006_Été.jpg", "website/trashed_215006_Été.jpg", "website/offline_215006_Été.jpg"}
				items := []map[string]any{}
				for i, path := range paths {
					item := candidateFixture(fmt.Sprintf("%08x-1234-4234-8234-123456789abc", i), "/external/photos/"+path, "IMAGE")
					if i == 1 {
						item["type"], item["duration"] = "VIDEO", 23800
					}
					if i == 7 {
						item["isTrashed"] = true
					}
					if i == 8 {
						item["isOffline"] = true
					}
					items = append(items, item)
				}
				candidateResponse(w, items, nil)
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, expose)
			baseline := map[string]consumerAsset{}
			for _, tc := range []struct {
				media, query string
				count        int
			}{
				{"", "", 3}, {"", "215006", 2}, {"", "éTÉ 215006", 2}, {"", "  DxO\u2003215006  ", 1}, {"", "215006 missing", 0}, {"", "website", 0}, {"", "filename-marker", 0},
				{"image", "", 2}, {"video", "", 1}, {"image", "éTÉ 215006", 1}, {"video", "éTÉ 215006", 1}, {"video", "DxO", 0}, {"image", "trashed", 0}, {"video", "private", 0},
			} {
				route := browseRoute
				if tc.media != "" {
					route += "&media=" + tc.media
				}
				if tc.query != "" {
					route += "&q=" + url.QueryEscape(tc.query)
				}
				out := httptest.NewRecorder()
				gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
				var page consumerPage
				if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Assets) != tc.count || page.NextCursor != nil {
					t.Fatalf("media %q query %q: %d %s", tc.media, tc.query, out.Code, out.Body)
				}
				if out.Header().Get("Access-Control-Allow-Origin") != "*" || out.Header().Get("Cache-Control") != "no-store" || out.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("security headers changed")
				}
				for _, asset := range page.Assets {
					if tc.media == "" && tc.query == "" {
						baseline[asset.ID] = asset
					}
					got, _ := json.Marshal(asset)
					want, _ := json.Marshal(baseline[asset.ID])
					if !bytes.Equal(got, want) || (tc.media != "" && asset.MediaType != tc.media) {
						t.Fatal("filter changed projection or admitted wrong media")
					}
				}
			}
			if logs.Len() != 0 {
				t.Fatalf("search logged data: %s", &logs)
			}
		})
	}
}

// TestFilenameSearchPagination proves sparse scan bounds and exact search/media binding.
func TestFilenameSearchPagination(t *testing.T) {
	for _, media := range []string{"", "image", "video"} {
		t.Run("media="+media, func(t *testing.T) { testFilenameSearchPagination(t, media) })
	}
}

// testFilenameSearchPagination checks the same bounded continuation contract for each media selector.
func testFilenameSearchPagination(t *testing.T, media string) {
	mediaQuery, providerMedia := "", `{"in":["IMAGE","VIDEO"]}`
	itemMedia := "IMAGE"
	if media != "" {
		mediaQuery = "&media=" + media
		itemMedia = strings.ToUpper(media)
		providerMedia = `{"in":["` + itemMedia + `"]}`
	}
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var q struct {
			Size   int
			Cursor string
			Filter map[string]any
		}
		if json.NewDecoder(r.Body).Decode(&q) != nil || q.Size != 1 {
			t.Error("invalid candidate page size")
		}
		if call > 1 && q.Cursor != fmt.Sprint(call-1) {
			t.Error("candidate continuation skipped")
		}
		types, _ := json.Marshal(q.Filter["type"])
		if len(q.Filter) != 4 || string(types) != providerMedia {
			t.Errorf("provider filter changed: %+v", q.Filter)
		}
		filename := "other.jpg"
		if call > 8 {
			filename = "MATCH.jpg"
		}
		var next any = fmt.Sprint(call)
		if call == 10 {
			next = nil
		}
		candidateResponse(w, []map[string]any{candidateFixture(testAsset, "/external/photos/website/"+filename, itemMedia)}, next)
	}))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, false)
	route := browseRoute + "&limit=1&q=match" + mediaQuery
	for pageNumber := 0; pageNumber < 3; pageNumber++ {
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
		var page consumerPage
		expected := 1
		if pageNumber == 0 {
			expected = 0
		}
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Assets) != expected || calls.Load() != int32(8+pageNumber) {
			t.Fatalf("page %d: %d %s calls %d", pageNumber, out.Code, out.Body, calls.Load())
		}
		if pageNumber == 2 {
			if page.NextCursor != nil {
				t.Fatal("terminal page has continuation")
			}
			break
		}
		if page.NextCursor == nil {
			t.Fatal("sparse page lost continuation")
		}
		cursor := "&cursor=" + url.QueryEscape(*page.NextCursor)
		changed := []string{"", "&q=other" + mediaQuery, "&q=MATCH" + mediaQuery, "&q=match+" + mediaQuery}
		for _, value := range []string{"", "image", "video"} {
			if value != media {
				search := "&q=match"
				if value != "" {
					search += "&media=" + value
				}
				changed = append(changed, search)
			}
		}
		for _, search := range changed {
			denied := httptest.NewRecorder()
			gateway.Config.Handler.ServeHTTP(denied, httptest.NewRequest("GET", browseRoute+search+cursor, nil))
			if denied.Code != 404 || calls.Load() != int32(8+pageNumber) {
				t.Fatal("cross-query cursor reached provider")
			}
		}
		route = browseRoute + "&limit=1&q=match" + mediaQuery + cursor
	}
}

// TestFilenameSearchInput applies shared search validation to assets and collections.
func TestFilenameSearchInput(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		candidateResponse(w, []map[string]any{}, nil)
	}))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, false)
	for _, suffix := range []string{"", "+++", "%E2%80%83", "a&q=b", "%zz", "%FF", "%00", "a%09b", "%C2%85", strings.Repeat("a", 257), url.QueryEscape(strings.Repeat("é", 129))} {
		for _, route := range []string{browseRoute + "&q=", "/catalog/collections?q="} {
			out := httptest.NewRecorder()
			gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route+suffix, nil))
			if out.Code != 404 || out.Body.String() != "not found\n" {
				t.Errorf("invalid search %q: %d", suffix, out.Code)
			}
		}
	}
	for _, route := range []string{"/catalog/assets/" + testAsset + "?q=photo"} {
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
		if out.Code != 404 {
			t.Error("search accepted on wrong route")
		}
	}
	if calls.Load() != 0 || logs.Len() != 0 {
		t.Fatal("invalid search reached provider or logs")
	}
	for _, query := range []string{strings.Repeat("a", 256), strings.Repeat("é", 128)} {
		for _, route := range []string{browseRoute + "&q=", "/catalog/collections?q="} {
			out := httptest.NewRecorder()
			gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route+url.QueryEscape(query), nil))
			if out.Code != 200 {
				t.Fatal("valid maximum search rejected")
			}
		}
	}
}
