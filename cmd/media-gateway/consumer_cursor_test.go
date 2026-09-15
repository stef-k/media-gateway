package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
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

// TestCursorBoundary checks signed state alterations before any provider I/O.
func TestCursorBoundary(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		candidateResponse(w, []map[string]any{}, nil)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	key := cursorKey{1}
	policy := config.Policy{Roots: []config.Root{{Name: "images", Path: "/external/photos"}}, Rules: []config.Rule{{Segment: "website", Media: []string{"image", "video"}}}}
	policyHash := fingerprint(immich.DiscoveryStreams(policy))
	hash := fingerprint(struct {
		Policy           [32]byte
		Root, Collection string
	}{policyHash, "images", "website"})
	good := *key.sign('a', hash, continuation{Provider: "next"})
	version := func() string {
		raw, _ := base64.RawURLEncoding.DecodeString(good)
		raw[0] = 2
		mac := hmac.New(sha256.New, key[:])
		mac.Write(raw[:len(raw)-32])
		copy(raw[len(raw)-32:], mac.Sum(nil))
		return base64.RawURLEncoding.EncodeToString(raw)
	}()
	for _, token := range []string{good[:len(good)-4] + "AAAA", version, *key.sign('c', hash, continuation{Provider: "next"}), *key.sign('a', policyHash, continuation{Provider: "next"}), *cursorKey{2}.sign('a', hash, continuation{Provider: "next"}), *key.sign('a', hash, continuation{Stream: 1, Provider: "next"}), *key.sign('a', hash, continuation{}), strings.Repeat("x", 2049), "raw-provider-cursor"} {
		resp, err := gateway.Client().Get(gateway.URL + browseRoute + "&cursor=" + url.QueryEscape(token))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Error("invalid cursor accepted")
		}
	}
	for _, route := range []string{"/internal/collections", "/internal/assets?root=images&collection=website/child"} {
		separator := "&"
		if route == "/internal/collections" {
			separator = "?"
		}
		resp, _ := gateway.Client().Get(gateway.URL + route + separator + "cursor=" + url.QueryEscape(good))
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Error("cross-query cursor accepted")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid cursor contacted provider")
	}
	resp, _ := gateway.Client().Get(gateway.URL + browseRoute + "&cursor=" + url.QueryEscape(good))
	resp.Body.Close()
	if resp.StatusCode != 200 || calls.Load() != 1 {
		t.Fatal("valid cursor rejected")
	}
	// Worst-case bounded provider UTF-8 remains inside the encoded gateway limit.
	longest := *key.sign('a', hash, continuation{Provider: strings.Repeat("x", 1024)})
	if len(longest) > 2048 {
		t.Fatal("cursor output too large")
	}
}

// TestSelectorBoundary covers canonical decoded collection syntax and both route kinds.
func TestSelectorBoundary(t *testing.T) {
	var calls atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		candidateResponse(w, []map[string]any{}, nil)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	for _, collection := range []string{"", "/website", "website/", "website//x", "website/.", "website/..", `website\x`, "website\n", string([]byte{255}), strings.Repeat("x", 2049)} {
		resp, _ := gateway.Client().Get(gateway.URL + "/internal/assets?root=images&collection=" + url.QueryEscape(collection))
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("invalid collection accepted: %q", collection)
		}
	}
	for _, suffix := range []string{"root=Images&collection=website", "root=missing&collection=website", "root=images&root=images&collection=website", "root=images", "collection=website"} {
		resp, _ := gateway.Client().Get(gateway.URL + "/internal/assets?" + suffix)
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Error("invalid root selector")
		}
	}
	for _, query := range []string{"limit=0", "limit=101", "limit=-1", "limit=%2B1", "limit=1.0", "limit= 1", "limit=", "limit=1&limit=2", "root=images", "collection=website", "cursor=", "cursor=a&cursor=b", "unknown=" + strings.Repeat("x", 8192)} {
		req := httptest.NewRequest("GET", "/internal/collections", nil)
		req.URL.RawQuery = query
		req.RemoteAddr = "127.0.0.1:1234"
		out := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(out, req)
		if out.Code != 404 {
			t.Errorf("invalid query accepted %q", query[:min(len(query), 40)])
		}
	}
	if calls.Load() != 0 {
		t.Fatal("malformed selectors reached provider")
	}
	for _, collection := range []string{"private", strings.Repeat("x", 2048), "website/a b", "website/%2e", "website/é"} {
		resp, _ := gateway.Client().Get(gateway.URL + "/internal/assets?root=images&collection=" + url.QueryEscape(collection))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Error("valid empty selector denied")
		}
	}
}

// TestCursorEntropyFailure makes missing startup randomness an explicit failure.
func TestCursorEntropyFailure(t *testing.T) {
	if _, err := newCursorKey(strings.NewReader("short")); err == nil {
		t.Fatal("short entropy accepted")
	}
}
