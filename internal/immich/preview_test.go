package immich

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPreviewStream proves the exported stream returns exact bytes and sanitized
// short-body failures; the HTTP gateway separately proves authorization ordering.
func TestPreviewStream(t *testing.T) {
	for _, truncated := range []bool{false, true} {
		client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.RequestURI() != "/api/assets/"+assetID+"/thumbnail?size=preview" || r.Header.Get("Accept-Encoding") != "identity" {
				t.Error("unexpected preview request")
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.Header().Set("Content-Length", "7")
			body := "preview"
			if truncated {
				body = "pre"
			}
			_, _ = io.WriteString(w, body)
		}, time.Second)
		preview, err := client.Preview(context.Background(), assetID)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(preview.Body)
		preview.Body.Close()
		if truncated {
			if !errors.Is(err, ErrTransport) {
				t.Fatalf("unsanitized stream error: %v", err)
			}
		} else if err != nil || string(body) != "preview" || preview.Length != 7 || preview.ContentType != "image/jpeg" {
			t.Fatalf("unexpected preview: %q %v", body, err)
		}
	}
}

// TestPreviewAmbiguousHeaders denies alternate encodings and partial/ambiguous
// representations even when the status, type and declared size appear acceptable.
func TestPreviewAmbiguousHeaders(t *testing.T) {
	for _, header := range []string{"Content-Encoding", "Content-Range", "Content-Type"} {
		t.Run(header, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/jpeg")
				w.Header().Set("Content-Length", "7")
				w.Header().Add(header, "private-body-marker")
				_, _ = io.WriteString(w, "preview")
			}, time.Second)
			preview, err := client.Preview(context.Background(), assetID)
			if preview != (Preview{}) || err != ErrPreview || strings.Contains(err.Error(), "private-body-marker") {
				t.Fatalf("ambiguous representation accepted: %v", err)
			}
		})
	}
}
