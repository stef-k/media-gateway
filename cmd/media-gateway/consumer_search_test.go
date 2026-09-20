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
			var baseline []consumerAsset
			for _, tc := range []struct {
				query string
				count int
			}{
				{"", 3}, {"215006", 2}, {"éTÉ 215006", 2}, {"  DxO\u2003215006  ", 1}, {"215006 missing", 0}, {"website", 0}, {"filename-marker", 0},
			} {
				route := browseRoute
				if tc.query != "" {
					route += "&q=" + url.QueryEscape(tc.query)
				}
				out := httptest.NewRecorder()
				gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
				var page consumerPage
				if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Assets) != tc.count || page.NextCursor != nil {
					t.Fatalf("query %q: %d %s", tc.query, out.Code, out.Body)
				}
				if out.Header().Get("Access-Control-Allow-Origin") != "*" || out.Header().Get("Cache-Control") != "no-store" || out.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Fatal("security headers changed")
				}
				if tc.query == "" {
					baseline = page.Assets
				}
				for i, asset := range page.Assets {
					got, _ := json.Marshal(asset)
					want, _ := json.Marshal(baseline[i])
					if !bytes.Equal(got, want) {
						t.Fatal("search changed projection")
					}
				}
			}
			if logs.Len() != 0 {
				t.Fatalf("search logged data: %s", &logs)
			}
		})
	}
}

// TestFilenameSearchPagination proves sparse scan bounds and exact search binding.
func TestFilenameSearchPagination(t *testing.T) {
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
		if len(q.Filter) != 4 {
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
		candidateResponse(w, []map[string]any{candidateFixture(testAsset, "/external/photos/website/"+filename, "IMAGE")}, next)
	}))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, false)
	route := browseRoute + "&limit=1&q=match"
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
		for _, search := range []string{"", "&q=other", "&q=MATCH", "&q=match+"} {
			denied := httptest.NewRecorder()
			gateway.Config.Handler.ServeHTTP(denied, httptest.NewRequest("GET", browseRoute+search+cursor, nil))
			if denied.Code != 404 || calls.Load() != int32(8+pageNumber) {
				t.Fatal("cross-query cursor reached provider")
			}
		}
		route = browseRoute + "&limit=1&q=match" + cursor
	}
}

// TestFilenameSearchInput rejects malformed searches before provider work.
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
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", browseRoute+"&q="+suffix, nil))
		if out.Code != 404 || out.Body.String() != "not found\n" {
			t.Errorf("invalid search %q: %d", suffix, out.Code)
		}
	}
	for _, route := range []string{"/catalog/collections?q=photo", "/catalog/assets/" + testAsset + "?q=photo"} {
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
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", browseRoute+"&q="+url.QueryEscape(query), nil))
		if out.Code != 200 {
			t.Fatal("valid maximum search rejected")
		}
	}
}
