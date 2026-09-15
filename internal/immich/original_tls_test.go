package immich

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
)

// TestOriginalTLS rejects an untrusted provider before sending the credential.
func TestOriginalTLS(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("untrusted TLS received a request") }))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	client := New(config.Provider{BaseURL: server.URL, RequestTimeout: time.Second}, testKey)
	defer client.CloseIdleConnections()
	original, err := client.Original(context.Background(), assetID)
	if err != ErrTransport || original != (Original{}) {
		t.Fatalf("untrusted provider accepted: %v", err)
	}
}
