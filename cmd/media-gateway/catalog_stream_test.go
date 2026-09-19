package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
)

// TestCollectionStreamTransitions preserves same-relative-path identities under
// distinct roots and resumes at the next deterministic stream without a provider cursor.
func TestCollectionStreamTransitions(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		var q struct {
			Size   int
			Cursor string
			Filter map[string]any
		}
		json.NewDecoder(r.Body).Decode(&q)
		pathFilter := q.Filter["originalPath"].(map[string]any)
		root := "/a/"
		if call == 2 {
			root = "/z/"
		}
		if pathFilter["startsWith"] != root || pathFilter["like"] != "/public/" || q.Size != 1 || q.Cursor != "" {
			t.Errorf("stream %d: %+v", call, q)
		}
		candidateResponse(w, []map[string]any{candidateFixture(testAsset, root+"public/a.jpg", "IMAGE")}, nil)
	}))
	defer provider.Close()
	client := immich.New(config.Provider{BaseURL: provider.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	policy := config.Policy{Roots: []config.Root{{Name: "z", Path: "/z"}, {Name: "a", Path: "/a"}}, Rules: []config.Rule{{Segment: "public", Media: []string{"image"}}}}
	handler := consumerHandler(client, policy, config.Privacy{ExposeSourceMetadata: true}, cursorKey{1}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	route := "/catalog/collections?limit=1"
	for _, root := range []string{"a", "z"} {
		req := httptest.NewRequest("GET", route, nil)
		req.RemoteAddr = "127.0.0.1:1234"
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		var page collectionPage
		if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Collections) != 1 || page.Collections[0] != (consumerCollection{root, "public"}) {
			t.Fatalf("stream response: %s", out.Body)
		}
		if root == "a" {
			if page.NextCursor == nil {
				t.Fatal("next stream lost")
			}
			route = "/catalog/collections?limit=1&cursor=" + url.QueryEscape(*page.NextCursor)
			// A changed policy definition rejects the old stream cursor before provider I/O.
			changed := config.Policy{Roots: policy.Roots, Rules: []config.Rule{{Segment: "changed", Media: []string{"image"}}}}
			req := httptest.NewRequest("GET", route, nil)
			req.RemoteAddr = "127.0.0.1:1234"
			denied := httptest.NewRecorder()
			consumerHandler(client, changed, config.Privacy{ExposeSourceMetadata: true}, cursorKey{1}, slog.New(slog.NewJSONHandler(io.Discard, nil))).ServeHTTP(denied, req)
			if denied.Code != 404 || calls.Load() != 1 {
				t.Fatal("changed stream fingerprint accepted")
			}
		} else if page.NextCursor != nil {
			t.Fatal("terminal cursor missing")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected stream work")
	}
}

// TestCatalogOverallDeadline proves the HTTP contract's 30-second work bound
// independently of a longer provider timeout; it also observes upstream cancellation.
func TestCatalogOverallDeadline(t *testing.T) {
	t.Parallel()
	stopped := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
		close(stopped)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, 45*time.Second)
	started := time.Now()
	resp, err := gateway.Client().Get(gateway.URL + browseRoute)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	elapsed := time.Since(started)
	if resp.StatusCode != 502 || elapsed < 29*time.Second || elapsed > 35*time.Second {
		t.Fatalf("deadline: status %d elapsed %s", resp.StatusCode, elapsed)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("upstream not canceled")
	}
}

// TestCatalogDetailRevocation reauthorizes a previously returned video reference.
func TestCatalogDetailRevocation(t *testing.T) {
	var state atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == representationTarget(testAsset, "original") || r.URL.Path == "/api/assets/"+testAsset+"/thumbnail" {
			if state.Load() != 0 {
				t.Error("revoked catalog reference opened bytes")
			}
			kind := "video/mp4"
			if r.URL.Path != representationTarget(testAsset, "original") {
				kind = "image/webp"
			}
			w.Header().Set("Content-Type", kind)
			w.Header().Set("Content-Length", "7")
			io.WriteString(w, "source!")
			return
		}
		item := candidateFixture(testAsset, "/external/photos/website/a.mp4", "VIDEO")
		switch state.Load() {
		case 1:
			item["originalPath"] = "/external/photos/private/a.mp4"
		case 2:
			item["isOffline"] = true
		case 3:
			item["isTrashed"] = true
		}
		if r.URL.Path == "/api/assets/"+testAsset {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(item)
			return
		}
		candidateResponse(w, []map[string]any{item}, nil)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	for i := int32(0); i < 4; i++ {
		state.Store(i)
		resp, err := gateway.Client().Get(gateway.URL + "/catalog/assets/" + testAsset)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := 404
		if i == 0 {
			want = 200
		}
		for _, variant := range []string{"preview", "original"} {
			response, err := gateway.Client().Get(gateway.URL + "/media/" + testAsset + "/" + variant)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
			if response.StatusCode != want {
				t.Fatal("stored catalog reference bypassed delivery reauthorization")
			}
		}
		if resp.StatusCode != want {
			t.Fatal("stored reference authorized revoked asset")
		}
	}
}
