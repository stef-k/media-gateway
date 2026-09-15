package main

import (
	"bytes"
	"fmt"
	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestOriginalFailures proves GET/HEAD revalidate representation headers before success.
func TestOriginalFailures(t *testing.T) {
	for _, status := range []int{200, 206, 301, 400, 401, 403, 404, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/assets/"+testAsset {
					metadata(w, "/external/photos/website/source.jpg", "IMAGE")
					return
				}
				if r.URL.RequestURI() != representationTarget(testAsset, "original") {
					t.Error("fallback fetch")
				}
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Location", "/private-marker")
				w.WriteHeader(status)
				io.WriteString(w, "private-body-marker")
			}))
			defer provider.Close()
			var logs bytes.Buffer
			gateway := gatewayFor(t, provider, &logs, time.Second)
			for _, method := range []string{"GET", "HEAD"} {
				req, _ := http.NewRequest(method, gateway.URL+"/media/"+testAsset+"/original", nil)
				resp, err := gateway.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				want, text := 502, "media unavailable\n"
				if status == 404 {
					want, text = 404, "not found\n"
				}
				if method == "HEAD" {
					text = ""
				}
				if err != nil || resp.StatusCode != want || string(body) != text {
					t.Fatalf("unsafe failure %d %q %v", resp.StatusCode, body, err)
				}
				assertPublicHeaders(t, resp)
			}
			if strings.Contains(logs.String(), "private-") || strings.Contains(logs.String(), provider.URL) {
				t.Fatal("provider diagnostics leaked")
			}
		})
	}
}

// TestImageRuleDenial proves a matching video-only convention cannot authorize an image.
func TestImageRuleDenial(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/assets/"+testAsset {
			t.Error("denied image fetched bytes")
		}
		metadata(w, "/external/photos/website/source.jpg", "IMAGE")
	}))
	defer provider.Close()
	client := immich.New(config.Provider{BaseURL: provider.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	policy := config.Policy{Roots: []config.Root{{Name: "images", Path: "/external/photos"}}, Rules: []config.Rule{{Segment: "website", Media: []string{"video"}}}}
	handler := deliveryHandler(client, policy, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, variant := range []string{"preview", "original"} {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/media/"+testAsset+"/"+variant, nil))
		if recorder.Code != 404 || recorder.Body.String() != "not found\n" {
			t.Fatal("image ignored rule media")
		}
	}
}
