package immich

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestOriginalReadInactivity proves progress extends a stream and a stall closes it.
func TestOriginalReadInactivity(t *testing.T) {
	for _, active := range []bool{true, false} {
		t.Run(map[bool]string{true: "active", false: "stalled"}[active], func(t *testing.T) {
			stopped := make(chan struct{})
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				defer close(stopped)
				w.Header().Set("Content-Type", "video/mp4")
				w.Header().Set("Content-Length", "8")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				if !active {
					<-r.Context().Done()
					return
				}
				for i := 0; i < 8; i++ {
					select {
					case <-r.Context().Done():
						return
					case <-time.After(30 * time.Millisecond):
					}
					io.WriteString(w, "x")
					w.(http.Flusher).Flush()
				}
			}, time.Second)
			client.streaming = originalHTTPWithInactivity(time.Second, 150*time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			source, err := client.VideoOriginal(ctx, assetID, ByteRange{})
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(source.Body)
			source.Body.Close()
			if active && (err != nil || string(body) != "xxxxxxxx") {
				t.Fatalf("active stream: %q %v", body, err)
			}
			if !active && err == nil {
				t.Fatal("stalled stream succeeded")
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("upstream not released")
			}
		})
	}
}

// TestVideoOriginalTypes preserves source formats, including Immich's MXF exception.
func TestVideoOriginalTypes(t *testing.T) {
	for _, kind := range []string{"video/mp4", "video/quicktime", "application/mxf", "application/octet-stream", "application/pdf", "video/", "video/mp4; codecs=avc1"} {
		t.Run(kind, func(t *testing.T) {
			client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", kind)
				w.Header().Set("Content-Length", "7")
				io.WriteString(w, "source!")
			}, time.Second)
			source, err := client.VideoOriginal(context.Background(), assetID, ByteRange{})
			valid := kind == "video/mp4" || kind == "video/quicktime" || kind == "application/mxf"
			if (err == nil) != valid {
				t.Fatalf("type validation: %v", err)
			}
			if err == nil {
				source.Body.Close()
			}
		})
	}
}

// TestVideoRangeTruncation refuses a successfully completed shorter 206 body.
func TestVideoRangeTruncation(t *testing.T) {
	client := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "7")
		w.Header().Set("Content-Range", "bytes 1-7/10")
		w.WriteHeader(206)
		io.WriteString(w, "raw")
	}, time.Second)
	requested, _ := ParseRange([]string{"bytes=1-7"})
	source, err := client.VideoOriginal(context.Background(), assetID, requested)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Body.Close()
	_, err = io.ReadAll(source.Body)
	if err != ErrTransport {
		t.Fatalf("truncation accepted: %v", err)
	}
}
