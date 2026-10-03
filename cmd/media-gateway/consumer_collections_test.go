package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
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

// TestCollectionRootPrivacy traverses identical streams for private and unknown
// roots, including empty budget-limited pages and terminal stream transitions.
func TestCollectionRootPrivacy(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var query struct {
			Cursor string
			Filter struct{ OriginalPath struct{ StartsWith string } }
		}
		if json.NewDecoder(r.Body).Decode(&query) != nil {
			t.Error("invalid discovery query")
		}
		items := []map[string]any{}
		var next any
		switch query.Filter.OriginalPath.StartsWith {
		case "/external/photos/":
			offset, _ := strconv.Atoi(query.Cursor)
			items = append(items, candidateFixture(testAsset, "/external/photos/website/public-result/a.jpg", "IMAGE"))
			if offset < 7 {
				next = fmt.Sprint(offset + 1)
			}
		case "/vault/":
			items = append(items, candidateFixture(testAsset, "/vault/private/a.jpg", "IMAGE"), candidateFixture(testAsset, "/vault/website/a.mp4", "VIDEO"))
			trashed := candidateFixture(testAsset, "/vault/website/trashed.jpg", "IMAGE")
			trashed["isTrashed"] = true
			offline := candidateFixture(testAsset, "/vault/website/offline.jpg", "IMAGE")
			offline["isOffline"] = true
			items = append(items, trashed, offline)
		default:
			t.Error("caller selected provider topology")
		}
		candidateResponse(w, items, next)
	}))
	defer provider.Close()
	client := immich.New(config.Provider{BaseURL: provider.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	policy := config.Policy{Roots: []config.Root{{Name: "images", Path: "/external/photos"}, {Name: "vault", Path: "/vault"}}, Rules: []config.Rule{{Segment: "website", Media: []string{"image"}}}}
	var logs bytes.Buffer
	handler := consumerHandler(client, policy, config.Privacy{}, cursorKey{1}, slog.New(slog.NewJSONHandler(&logs, nil)))
	for _, root := range []string{"vault", "unknown", "images"} {
		route := "/catalog/collections?root=" + root + "&q=website"
		for pageNumber := 0; pageNumber < 2; pageNumber++ {
			before := calls.Load()
			out := httptest.NewRecorder()
			handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
			var page collectionPage
			count, work := 0, int32(8)
			if root == "images" && pageNumber == 0 {
				count = 1
			}
			if pageNumber == 1 {
				work = 1
			}
			if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Collections) != count || calls.Load()-before != work || (page.NextCursor != nil) != (pageNumber == 0) {
				t.Fatalf("root %s page %d: %d %s", root, pageNumber, out.Code, out.Body)
			}
			if count == 1 && page.Collections[0].Root != root {
				t.Fatal("root selector did not filter public results")
			}
			if page.NextCursor != nil {
				route += "&cursor=" + url.QueryEscape(*page.NextCursor)
			} else if out.Body.String() != `{"collections":[],"next_cursor":null}` {
				t.Fatal("terminal empty result disclosed private topology")
			}
		}
	}
	if logs.Len() != 0 {
		t.Fatal("root filters entered routine logs")
	}
}

// TestCollectionFilterInput denies malformed selectors and unknown keys before I/O.
func TestCollectionFilterInput(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayFor(t, provider, &logs, time.Second)
	for _, selector := range []string{"root=", "root=images&root=images", "root=Images", "root=-images", "root=images-", "root=a/b", "root=%FF", "root=%00", "root=" + strings.Repeat("a", 65), "media=", "media=image&media=video", "media=IMAGE", "media=audio", "media=%zz", "collection=website", "sort=path", "filter=%7B%7D", "url=http://attacker.invalid"} {
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", "/catalog/collections?"+selector, nil))
		if out.Code != 404 || out.Body.String() != "not found\n" {
			t.Errorf("invalid filter %s: %d", selector, out.Code)
		}
	}
	for _, route := range []string{browseRoute + "&media=image", "/catalog/assets/" + testAsset + "?media=image", "/catalog/assets?root=unknown&collection=website"} {
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
		if out.Code != 404 {
			t.Fatal("asset selector semantics changed")
		}
	}
	if calls.Load() != 0 || logs.Len() != 0 {
		t.Fatal("invalid filter reached provider or logs")
	}
}

// TestCollectionMediaFilter narrows provider types/streams, then rejects their
// overmatches and chooses only an eligible representative of the requested type.
func TestCollectionMediaFilter(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var query struct {
			Filter struct {
				Type         struct{ In []string }
				OriginalPath struct{ StartsWith string }
			}
		}
		if json.NewDecoder(r.Body).Decode(&query) != nil || len(query.Filter.Type.In) != 1 {
			t.Error("media predicate did not narrow candidates")
		}
		items := []map[string]any{
			candidateFixture(testAsset, "/external/photos/website/mixed/clip.mp4", "VIDEO"),
			candidateFixture("87654321-1234-4234-8234-123456789abc", "/external/photos/website/mixed/photo.jpg", "IMAGE"),
			candidateFixture(testAsset, "/external/photos/website/video-only/clip.mp4", "VIDEO"),
			candidateFixture(testAsset, "/external/photos/website/image-only/photo.jpg", "IMAGE"),
			candidateFixture(testAsset, "/external/photos/private/photo.jpg", "IMAGE"),
			candidateFixture(testAsset, "/films/public/wrong-type.jpg", "IMAGE"),
		}
		if query.Filter.OriginalPath.StartsWith == "/films/" {
			if query.Filter.Type.In[0] != "VIDEO" {
				t.Error("image filter scanned a video-only stream")
			}
			items = []map[string]any{candidateFixture(testAsset, "/films/public/clip.mp4", "VIDEO")}
		}
		candidateResponse(w, items, nil)
	}))
	defer provider.Close()
	client := immich.New(config.Provider{BaseURL: provider.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	policy := config.Policy{Roots: []config.Root{{Name: "images", Path: "/external/photos"}, {Name: "films", Path: "/films"}}, Rules: []config.Rule{
		{Segment: "website", Media: []string{"image", "video"}, Roots: []string{"images"}},
		{Segment: "public", Media: []string{"video"}, Roots: []string{"films"}},
	}}
	handler := consumerHandler(client, policy, config.Privacy{}, cursorKey{1}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, media := range []string{"image", "video"} {
		before := calls.Load()
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", "/catalog/collections?media="+media, nil))
		var page struct{ Collections []map[string]string }
		count, work := 2, int32(1)
		if media == "video" {
			count, work = 3, 2
		}
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Collections) != count || calls.Load()-before != work {
			t.Fatalf("media %s: %d %s", media, out.Code, out.Body)
		}
		for _, collection := range page.Collections {
			if collection["representative_media_type"] != media || strings.Contains(collection["collection_path"], "wrong-type") || (media == "image" && collection["collection_path"] == "website/video-only") || (media == "video" && collection["collection_path"] == "website/image-only") {
				t.Fatalf("media overmatch: %+v", collection)
			}
		}
	}
}

// TestCollectionSearchCursor binds exact decoded search and media, and resumes
// sparse discovery after the shared eight-call budget without losing a candidate.
func TestCollectionSearchCursor(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var query struct{ Cursor string }
		if json.NewDecoder(r.Body).Decode(&query) != nil || (call > 1 && query.Cursor != fmt.Sprint(call-1)) {
			t.Error("discovery continuation lost")
		}
		path, next := "winter", any(fmt.Sprint(call))
		if call == 9 {
			path, next = "summer", nil
		}
		candidateResponse(w, []map[string]any{candidateFixture(testAsset, "/external/photos/website/"+path+"/a.jpg", "IMAGE")}, next)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	route := "/catalog/collections?limit=1&q=summer&media=image&root=images"
	out := httptest.NewRecorder()
	gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
	var page collectionPage
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Collections) != 0 || page.NextCursor == nil || calls.Load() != 8 {
		t.Fatalf("sparse discovery: %d %s", out.Code, out.Body)
	}
	cursor := "&cursor=" + url.QueryEscape(*page.NextCursor)
	for _, selectors := range []string{"", "q=summer", "media=image", "q=summer&media=image", "q=SUMMER&media=image&root=images", "q=summer+&media=image&root=images", "q=summer&media=video&root=images", "q=summer&media=image&root=unknown"} {
		denied := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(denied, httptest.NewRequest("GET", "/catalog/collections?"+selectors+cursor, nil))
		if denied.Code != 404 || calls.Load() != 8 {
			t.Fatal("cross-query cursor reached provider")
		}
	}
	out = httptest.NewRecorder()
	gateway.Config.Handler.ServeHTTP(out, httptest.NewRequest("GET", route+cursor, nil))
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Collections) != 1 || page.NextCursor != nil || calls.Load() != 9 {
		t.Fatal("sparse search lost its later match")
	}
}
