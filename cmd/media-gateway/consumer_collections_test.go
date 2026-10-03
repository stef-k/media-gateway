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

// TestCollectionSearch matches only authorized logical identity and projects a
// representative from its discovery candidate without any representation probe.
func TestCollectionSearch(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var query struct{ WithExif bool }
		if json.NewDecoder(r.Body).Decode(&query) != nil || query.WithExif || r.URL.Path != "/api/search/metadata" {
			t.Error("discovery fetched more than policy facts")
		}
		paths := []string{"website/Été Summer/a.jpg", "website/Été Summer/b.mp4", "website/Été Winter/c.jpg", "website/100%_real/d.jpg", "private/Été Summer/e.jpg", "Website/Été Summer/f.jpg", "../website/Été Summer/g.jpg", "website/trashed/h.jpg", "website/offline/i.jpg"}
		items := []map[string]any{}
		for i, path := range paths {
			item := candidateFixture(fmt.Sprintf("%08x-1234-4234-8234-123456789abc", i), "/external/photos/"+path, "IMAGE")
			if i == 1 {
				item["type"] = "VIDEO"
			}
			item["isTrashed"], item["isOffline"] = i == 7, i == 8
			items = append(items, item)
		}
		candidateResponse(w, items, nil)
	}))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayWithPrivacy(t, provider, &logs, time.Second, false)
	queries := []struct {
		value string
		count int
	}{{"", 3}, {"IMAGES", 3}, {"  suMMer\u2003ÉTÉ  ", 1}, {"images/website été", 2}, {"100%_", 1}, {"a.jpg", 0}, {"external/photos", 0}, {"private", 0}, {"trashed", 0}, {"offline", 0}}
	for _, query := range queries {
		route := "/catalog/collections"
		if query.value != "" {
			route += "?q=" + url.QueryEscape(query.value)
		}
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
		var page struct {
			Collections []map[string]string
			NextCursor  *string `json:"next_cursor"`
		}
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Collections) != query.count || page.NextCursor != nil {
			t.Fatalf("query %q: %d %s", query.value, out.Code, out.Body)
		}
		for _, collection := range page.Collections {
			if len(collection) != 4 || collection["root"] != "images" || collection["representative_media_type"] != "image" || !strings.HasPrefix(collection["representative_preview_path"], "/media/") || !strings.HasSuffix(collection["representative_preview_path"], "/preview") {
				t.Fatalf("unsafe collection projection: %+v", collection)
			}
		}
		if out.Header().Get("Access-Control-Allow-Origin") != "*" || out.Header().Get("Cache-Control") != "no-store" || strings.Contains(out.Body.String(), "/external/") {
			t.Fatal("catalog privacy/HTTP contract changed")
		}
	}
	if calls.Load() != int32(len(queries)) || logs.Len() != 0 {
		t.Fatal("collection search added provider work or routine logs")
	}
}
