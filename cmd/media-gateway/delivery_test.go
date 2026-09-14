package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
)

// Synthetic provider values are distinctive so failures can prove non-disclosure.
const testAsset = "12345678-1234-4234-8234-123456789abc"
const testRoute = "/media/" + testAsset + "/preview"
const testKey = "provider-secret-marker"

// gatewayFor uses the real client, policy evaluator, handler and server bounds.
func gatewayFor(t *testing.T, provider *httptest.Server, logs io.Writer, timeout time.Duration) *httptest.Server {
	t.Helper()
	return gatewayWithCoordinates(t, provider, logs, timeout, false)
}

// gatewayWithCoordinates exercises the same startup wiring with an explicit capability.
func gatewayWithCoordinates(t *testing.T, provider *httptest.Server, logs io.Writer, timeout time.Duration, enabled bool) *httptest.Server {
	t.Helper()
	client := immich.New(config.Provider{BaseURL: provider.URL, RequestTimeout: timeout}, testKey)
	t.Cleanup(client.CloseIdleConnections)
	policy := config.Policy{AllowedRoots: []string{"/external/photos"}, Rules: []config.Rule{{Segment: "website", Media: []string{"image", "video"}}}}
	server := httptest.NewUnstartedServer(nil)
	server.Config = newServer(gatewayHandler(client, policy, config.Consumer{ExposeCoordinates: enabled}, slog.New(slog.NewJSONHandler(logs, nil))))
	server.Start()
	t.Cleanup(server.Close)
	return server
}

// metadata writes only fake policy inputs; real Immich qualification belongs to #18.
func metadata(w http.ResponseWriter, path, media string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"id": testAsset, "originalPath": path, "type": media, "isTrashed": false, "isOffline": false})
}

// TestDeliveryRevalidation proves exact bytes, HEAD parity, fixed upstream targets,
// credential isolation, ignored caller selectors and immediate metadata revocation.
func TestDeliveryRevalidation(t *testing.T) {
	var private atomic.Bool
	var assets, previews atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host == "attacker.invalid" || r.Method != "GET" || r.Header.Get("x-api-key") != testKey || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("X-Caller") != "" || r.Header.Get("Range") != "" || r.Header.Get("If-None-Match") != "" {
			t.Error("unexpected upstream method or headers")
		}
		switch r.URL.RequestURI() {
		case "/api/assets/" + testAsset:
			assets.Add(1)
			path := "/external/photos/website/photo.jpg"
			if private.Load() {
				path = "/external/photos/private/photo.jpg"
			}
			metadata(w, path, "IMAGE")
		case "/api/assets/" + testAsset + "/thumbnail?size=preview":
			previews.Add(1)
			w.Header().Set("Content-Type", "image/webp")
			w.Header().Set("Content-Length", "7")
			w.Header().Set("Set-Cookie", testKey)
			w.Header().Set("Content-Disposition", "attachment; filename=private-marker")
			w.Header().Set("Cache-Control", "public, max-age=9999")
			w.Header().Set("ETag", "private-marker")
			_, _ = io.WriteString(w, "preview")
		default:
			t.Errorf("unexpected upstream route %q", r.URL.RequestURI())
		}
	}))
	defer provider.Close()
	var logs bytes.Buffer
	gateway := gatewayFor(t, provider, &logs, time.Second)
	var getHeaders http.Header
	for i, method := range []string{"GET", "HEAD", "GET", "HEAD"} {
		private.Store(i >= 2)
		req, _ := http.NewRequest(method, gateway.URL+testRoute+"?size=original&url=http://attacker.invalid/&key=caller", nil)
		req.Host = "attacker.invalid"
		for _, name := range []string{"Authorization", "Cookie", "X-Caller", "x-api-key", "Range", "If-None-Match"} {
			req.Header.Set(name, "caller-secret")
		}
		resp, err := gateway.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		wantStatus, wantBody := 200, "preview"
		if i >= 2 {
			wantStatus, wantBody = 404, "not found\n"
		}
		if method == "HEAD" {
			wantBody = ""
		}
		if err != nil || resp.StatusCode != wantStatus || string(body) != wantBody {
			t.Fatalf("%s: %d %q %v", method, resp.StatusCode, body, err)
		}
		if i == 0 {
			getHeaders = resp.Header
		}
		if i == 1 {
			for _, name := range []string{"Content-Type", "Content-Length", "Cache-Control", "X-Content-Type-Options"} {
				if resp.Header.Get(name) != getHeaders.Get(name) {
					t.Errorf("HEAD differs: %s", name)
				}
			}
		}
		assertPublicHeaders(t, resp)
	}
	if assets.Load() != 4 || previews.Load() != 2 || logs.Len() != 0 {
		t.Fatalf("lookups=%d previews=%d logs=%s", assets.Load(), previews.Load(), &logs)
	}
}

// assertPublicHeaders allows net/http's Date and only deliberately constructed fields.
func assertPublicHeaders(t *testing.T, resp *http.Response) {
	t.Helper()
	for name := range resp.Header {
		switch name {
		case "Date", "Content-Type", "Content-Length", "Cache-Control", "X-Content-Type-Options":
		default:
			t.Errorf("unexpected public header: %s", name)
		}
	}
	if resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("missing cache/sniff protection")
	}
}

// TestDeliveryDenials proves policy failures and unsupported routes never fetch bytes.
func TestDeliveryDenials(t *testing.T) {
	for _, tc := range []struct {
		name, path, media, route, method string
		metadataStatus                   int
	}{
		{name: "private", path: "/external/photos/private/photo.jpg"},
		{name: "outside", path: "/outside/website/photo.jpg"},
		{name: "near miss", path: "/external/photos/website-old/photo.jpg"},
		{name: "malformed", path: "/external/photos/website/../photo.jpg"},
		{name: "video", path: "/external/photos/website/photo.jpg", media: "VIDEO"},
		{name: "unknown type", path: "/external/photos/website/photo.jpg", media: "OTHER"},
		{name: "missing", metadataStatus: 404},
		{name: "missing or inaccessible v3", metadataStatus: 400},
		{name: "invalid UUID", route: "/media/known-private/preview"},
		{name: "escaped ID", route: "/media/%31" + testAsset[1:] + "/preview"},
		{name: "encoded slash", route: "/media/" + testAsset + "%2fpreview"},
		{name: "dot path", route: "/media/../" + testAsset + "/preview"},
		{name: "double slash", route: "/media//" + testAsset + "/preview"},
		{name: "URL path", route: "/media/http://attacker.invalid/preview"},
		{name: "original", route: "/media/" + testAsset + "/original"},
		{name: "health", route: "/health"},
		{name: "search", route: "/internal/search"},
		{name: "metadata route", route: "/media/" + testAsset},
		{name: "post", method: "POST"}, {name: "put", method: "PUT"}, {name: "delete", method: "DELETE"},
		{name: "options", method: "OPTIONS"}, {name: "connect", method: "CONNECT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/assets/"+testAsset {
					t.Error("denial fetched preview")
				}
				if tc.metadataStatus != 0 {
					w.WriteHeader(tc.metadataStatus)
					_, _ = io.WriteString(w, testKey+" /private/path provider-body-marker")
					return
				}
				media := tc.media
				if media == "" {
					media = "IMAGE"
				}
				metadata(w, tc.path, media)
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayFor(t, provider, &logs, time.Second)
			route, method := tc.route, tc.method
			if route == "" {
				route = testRoute
			}
			if method == "" {
				method = "GET"
			}
			req, _ := http.NewRequest(method, gateway.URL+route, nil)
			resp, err := gateway.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 404 || string(body) != "not found\n" {
				t.Fatalf("denial: %d %q %v", resp.StatusCode, body, err)
			}
			if (tc.route != "" || tc.method != "") && calls.Load() != 0 {
				t.Error("invalid request contacted provider")
			}
			if logs.Len() != 0 {
				t.Errorf("routine denial logged: %s", &logs)
			}
			assertPublicHeaders(t, resp)
		})
	}
}

// TestPreviewFailures verifies bounded status/header validation and no redirect fallback.
func TestPreviewFailures(t *testing.T) {
	for _, tc := range []struct {
		name, kind, length, location string
		status, want                 int
	}{
		{name: "bad request", status: 400}, {name: "missing", status: 404, want: 404}, {name: "auth", status: 401}, {name: "forbidden", status: 403}, {name: "provider", status: 500},
		{name: "same origin redirect", status: 302, location: "/api/assets/" + testAsset + "/original"},
		{name: "cross origin redirect", status: 307, location: "cross"},
		{name: "html", kind: "text/html"}, {name: "svg", kind: "image/svg+xml"}, {name: "png", kind: "image/png"},
		{name: "parameters", kind: "image/jpeg; filename=private-marker"},
		{name: "missing length", length: "missing"}, {name: "invalid length", length: "invalid"},
		{name: "zero length", length: "0"}, {name: "negative length", length: "-1"}, {name: "oversized", length: "16777217"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			escape := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("redirect followed") }))
			defer escape.Close()
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/api/assets/"+testAsset {
					metadata(w, "/external/photos/website/private-path-marker.jpg", "IMAGE")
					return
				}
				if r.URL.RequestURI() != "/api/assets/"+testAsset+"/thumbnail?size=preview" {
					t.Error("fallback contacted")
				}
				kind, length, status := tc.kind, tc.length, tc.status
				if kind == "" {
					kind = "image/jpeg"
				}
				if length == "" {
					length = "20"
				}
				if status == 0 {
					status = 200
				}
				w.Header().Set("Content-Type", kind)
				if length != "missing" {
					w.Header().Set("Content-Length", length)
				}
				location := tc.location
				if location == "cross" {
					location = escape.URL
				}
				if location != "" {
					w.Header().Set("Location", location)
				}
				w.Header().Set("Set-Cookie", testKey)
				w.WriteHeader(status)
				w.(http.Flusher).Flush()
				_, _ = io.WriteString(w, "provider-body-marker")
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayFor(t, provider, &logs, time.Second)
			resp, err := gateway.Client().Get(gateway.URL + testRoute)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			want := tc.want
			if want == 0 {
				want = 502
			}
			wantBody := "media unavailable\n"
			if want == 404 {
				wantBody = "not found\n"
			}
			if err != nil || resp.StatusCode != want || string(body) != wantBody || calls.Load() != 2 {
				t.Fatalf("response %d %q err=%v calls=%d", resp.StatusCode, body, err, calls.Load())
			}
			assertPublicHeaders(t, resp)
			for _, secret := range []string{testKey, provider.URL, "private-path-marker", "provider-body-marker"} {
				if strings.Contains(string(body)+logs.String(), secret) {
					t.Error("private data leaked")
				}
			}
		})
	}
}
