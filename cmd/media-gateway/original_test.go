package main

import (
	"bytes"
	"fmt"
	"io"
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
