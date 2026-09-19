package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestPublicCatalogCORS proves remote browser GETs can read success and denial
// responses without granting credentials or weakening current publication checks.
func TestPublicCatalogCORS(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		candidateResponse(w, []map[string]any{candidateFixture(testAsset, "/external/photos/website/a.jpg", "IMAGE")}, nil)
	}))
	defer provider.Close()
	gateway := gatewayFor(t, provider, io.Discard, time.Second)
	for _, tc := range []struct {
		route  string
		status int
	}{
		{"/catalog/collections", 200},
		{"/catalog/assets?root=images&collection=website", 200},
		{"/catalog/assets/" + testAsset, 200},
		{"/catalog/assets?unknown=value", 404},
	} {
		req := httptest.NewRequest("GET", tc.route, nil)
		req.RemoteAddr = "192.0.2.1:1234"
		req.Header.Set("Origin", "https://editor.example")
		recorder := httptest.NewRecorder()
		gateway.Config.Handler.ServeHTTP(recorder, req)
		if recorder.Code != tc.status || recorder.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s: status %d, headers %v", tc.route, recorder.Code, recorder.Header())
		}
		if recorder.Header().Get("Access-Control-Allow-Credentials") != "" || recorder.Header().Get("Set-Cookie") != "" {
			t.Fatal("catalog grants credentials")
		}
	}
}
