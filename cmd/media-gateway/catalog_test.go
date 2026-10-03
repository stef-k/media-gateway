package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
)

const browseRoute = "/catalog/assets?root=images&collection=website"

// TestCatalogFill consumes every candidate, requests only remaining slots and
// fails the whole request if a later page fails after an eligible result.
func TestCatalogFill(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				call := int(calls.Add(1))
				var q struct {
					Size   int
					Cursor string
				}
				if json.NewDecoder(r.Body).Decode(&q) != nil {
					t.Error("invalid provider request")
				}
				sizes := []int{3, 2, 1}
				if call > len(sizes) || q.Size != sizes[call-1] {
					t.Errorf("request size: %+v call %d", q, call)
					return
				}
				if call > 1 && q.Cursor != fmt.Sprint(call) {
					t.Error("provider continuation lost")
				}
				if fail && call == 2 {
					http.Error(w, "private-marker", 503)
					return
				}
				items := []map[string]any{candidateFixture(fmt.Sprintf("%08x-1234-4234-8234-123456789abc", call), "/external/photos/website/file.jpg", "IMAGE")}
				for len(items) < q.Size {
					items = append(items, candidateFixture(testAsset, "/external/photos/private/file.jpg", "IMAGE"))
				}
				var next any
				if call < 3 {
					next = fmt.Sprint(call + 1)
				}
				candidateResponse(w, items, next)
			}))
			defer provider.Close()
			gateway := gatewayFor(t, provider, io.Discard, time.Second)
			resp, err := gateway.Client().Get(gateway.URL + browseRoute + "&limit=3")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if fail {
				body, _ := io.ReadAll(resp.Body)
				if resp.StatusCode != 502 || string(body) != "media unavailable\n" {
					t.Fatal("partial success escaped")
				}
				return
			}
			var page consumerPage
			if json.NewDecoder(resp.Body).Decode(&page) != nil || resp.StatusCode != 200 || len(page.Assets) != 3 || page.NextCursor != nil || calls.Load() != 3 {
				t.Fatalf("fill: %+v calls %d", page, calls.Load())
			}
		})
	}
}

// TestCatalogBudget proves finite sparse work, empty continuation and page resumption.
func TestCatalogBudget(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var q struct {
			Size   int
			Cursor string
		}
		json.NewDecoder(r.Body).Decode(&q)
		if q.Size != 25 {
			t.Errorf("default size %d", q.Size)
		}
		if call == 9 && q.Cursor != "offset-8" {
			t.Error("resume skipped provider state")
		}
		candidateResponse(w, []map[string]any{candidateFixture(testAsset, "/external/photos/private/x.jpg", "IMAGE")}, fmt.Sprintf("offset-%d", call))
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	route := browseRoute
	for pageNumber := 1; pageNumber <= 2; pageNumber++ {
		resp, err := gateway.Client().Get(gateway.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		var page consumerPage
		err = json.NewDecoder(resp.Body).Decode(&page)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || len(page.Assets) != 0 || page.NextCursor == nil || calls.Load() != int32(8*pageNumber) {
			t.Fatalf("budget: %+v calls %d", page, calls.Load())
		}
		if strings.Contains(*page.NextCursor, "offset") {
			t.Fatal("raw provider cursor escaped")
		}
		route = browseRoute + "&cursor=" + url.QueryEscape(*page.NextCursor)
	}
}

// TestCollectionPagination exercises in-page deduplication, at-least-once identity
// across pages, stream transitions, and discovery's deliberately minimal decoding.
func TestCollectionPagination(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var q struct {
			Size     int
			WithExif bool
			Cursor   string
			Filter   map[string]any
		}
		json.NewDecoder(r.Body).Decode(&q)
		if q.WithExif || r.URL.Path != "/api/search/metadata" {
			t.Error("wrong discovery boundary")
		}
		if q.Filter["originalPath"].(map[string]any)["like"] != "/website/" {
			t.Error("missing segment filter")
		}
		item := candidateFixture(testAsset, "/external/photos/website/a.jpg", "VIDEO")
		item["width"], item["duration"], item["fileCreatedAt"], item["exifInfo"] = "malformed", -1, "invalid", true
		switch call {
		case 1:
			if q.Size != 2 || q.Cursor != "" {
				t.Error("first discovery size")
			}
			sibling := candidateFixture("87654321-1234-4234-8234-123456789abc", "/external/photos/website/sibling.jpg", "IMAGE")
			candidateResponse(w, []map[string]any{item, sibling}, "next")
		case 2:
			if q.Size != 1 || q.Cursor != "next" {
				t.Error("dedup remaining slot")
			}
			item["originalPath"] = "/external/photos/website/day2/b.jpg"
			candidateResponse(w, []map[string]any{item}, "later")
		case 3:
			if q.Cursor != "later" {
				t.Error("cross-page continuation")
			}
			item["id"], item["type"] = "87654321-1234-4234-8234-123456789abc", "IMAGE"
			candidateResponse(w, []map[string]any{item}, nil)
		default:
			t.Error("excess discovery")
			candidateResponse(w, []map[string]any{}, nil)
		}
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	resp, err := gateway.Client().Get(gateway.URL + "/catalog/collections?limit=2")
	if err != nil {
		t.Fatal(err)
	}
	var first collectionPage
	err = json.NewDecoder(resp.Body).Decode(&first)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 || len(first.Collections) != 2 || first.NextCursor == nil {
		t.Fatalf("first: %+v %v", first, err)
	}
	resp, err = gateway.Client().Get(gateway.URL + "/catalog/collections?limit=2&cursor=" + url.QueryEscape(*first.NextCursor))
	if err != nil {
		t.Fatal(err)
	}
	var second collectionPage
	err = json.NewDecoder(resp.Body).Decode(&second)
	resp.Body.Close()
	if err != nil || len(second.Collections) != 1 || second.Collections[0].Root != first.Collections[0].Root || second.Collections[0].CollectionPath != first.Collections[0].CollectionPath || second.NextCursor != nil || calls.Load() != 3 {
		t.Fatalf("at least once: %+v %v", second, err)
	}
	if first.Collections[0].RepresentativePreviewPath != testRoute || first.Collections[0].RepresentativeMediaType != "video" || second.Collections[0].RepresentativePreviewPath != "/media/87654321-1234-4234-8234-123456789abc/preview" || second.Collections[0].RepresentativeMediaType != "image" {
		t.Fatal("representative became collection identity")
	}
}

// TestCatalogMembershipAndProjection rejects path-filter overmatches and derives
// filename/context only from authorized paths, with accurate current capabilities.
func TestCatalogMembershipAndProjection(t *testing.T) {
	paths := []string{"/external/photos/website/a.jpg", "/external/photos/website/b.mp4", "/external/photos/website/child/c.jpg", "/external/photos/website-old/x.jpg", "/external/photos/Website/x.jpg", "/external/photos/wébsite/x.jpg", "/external/photos-old/website/x.jpg", "/outside/website/x.jpg", "/external/photos/private/x.jpg"}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []map[string]any{}
		for i, path := range paths {
			media := "IMAGE"
			if i == 1 {
				media = "VIDEO"
			}
			item := candidateFixture(fmt.Sprintf("%08x-1234-4234-8234-123456789abc", i), path, media)
			if i == 1 {
				item["duration"] = 23800
			}
			items = append(items, item)
		}
		candidateResponse(w, items, nil)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	for _, media := range []string{"", "image", "video"} {
		route, count := browseRoute, 2
		if media != "" {
			route, count = route+"&media="+media, 1
		}
		resp, err := gateway.Client().Get(gateway.URL + route)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var page consumerPage
		if resp.StatusCode != 200 || json.Unmarshal(body, &page) != nil || len(page.Assets) != count {
			t.Fatalf("media %q membership: %s", media, body)
		}
		for _, item := range page.Assets {
			if item.Root != "images" || item.CollectionPath != "website" || item.Latitude != nil || item.Longitude != nil || (media != "" && item.MediaType != media) {
				t.Fatalf("projection: %+v", item)
			}
			if item.MediaType == "image" && (item.Filename != "a.jpg" || item.PreviewPath == nil || item.OriginalPath == nil || *item.OriginalPath != "/media/"+item.ID+"/original" || item.DurationMS != nil) {
				t.Fatal("image capability")
			}
			if item.MediaType == "video" && (item.Filename != "b.mp4" || item.PreviewPath == nil || item.OriginalPath == nil || item.DurationMS == nil || *item.DurationMS != 23800) {
				t.Fatal("video capability/units")
			}
		}
		for _, marker := range []string{"originalFileName", "filename-marker", "/external/", "provider-marker", testKey, "exifInfo", "child"} {
			if strings.Contains(string(body), marker) {
				t.Fatalf("leaked %s", marker)
			}
		}
	}
}

// TestAssetMediaPolicy proves a requested provider type cannot override the rule's media scope.
func TestAssetMediaPolicy(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		candidateResponse(w, []map[string]any{
			candidateFixture(testAsset, "/external/photos/website/wrong-type.jpg", "IMAGE"),
			candidateFixture("87654321-1234-4234-8234-123456789abc", "/external/photos/website/clip.mp4", "VIDEO"),
		}, nil)
	}))
	defer provider.Close()
	client := immich.New(config.Provider{BaseURL: provider.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	policy := config.Policy{Roots: []config.Root{{Name: "images", Path: "/external/photos"}}, Rules: []config.Rule{{Segment: "website", Media: []string{"video"}, Roots: []string{"images"}}}}
	handler := consumerHandler(client, policy, config.Privacy{}, cursorKey{1}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	for _, media := range []string{"", "image", "video"} {
		route, count := browseRoute, 1
		if media != "" {
			route += "&media=" + media
		}
		if media == "image" {
			count = 0
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", route, nil))
		var page consumerPage
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Assets) != count || page.NextCursor != nil {
			t.Fatalf("media %q bypassed publication rule: %d %s", media, out.Code, out.Body)
		}
	}
}
