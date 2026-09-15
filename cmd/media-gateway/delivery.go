package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
	"github.com/stef-k/media-gateway/internal/publication"
)

// deliveryTimeout caps combined metadata/preview work; the server write deadline
// separately bounds blocked downstream writes. Each provider call also has a timeout.
const deliveryTimeout = 60 * time.Second

// deliveryHandler owns public authorization; the concrete client owns private HTTP.
// Policy comes from config.Load and remains immutable for the process lifetime.
func deliveryHandler(client *immich.Client, policy config.Policy, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		id, ok := previewRoute(r)
		if !ok {
			publicError(w, r, http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), deliveryTimeout)
		defer cancel()
		asset, err := client.Asset(ctx, id)
		if err != nil {
			deliveryError(w, r, logger, err)
			return
		}
		if _, eligible := publication.Evaluate(policy, asset.OriginalPath, asset.Media); asset.Media != "image" || !eligible {
			publicError(w, r, http.StatusNotFound)
			return
		}
		preview, err := client.Preview(ctx, id)
		if err != nil {
			deliveryError(w, r, logger, err)
			return
		}
		defer preview.Body.Close()
		w.Header().Set("Content-Type", preview.ContentType)
		w.Header().Set("Content-Length", strconv.FormatInt(preview.Length, 10))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		if _, err := io.Copy(w, preview.Body); err != nil {
			// Headers may already be on the wire. Abort instead of returning a
			// successfully completed partial image or appending a plaintext error.
			if r.Context().Err() == nil {
				logger.Warn("preview stream failed")
			}
			panic(http.ErrAbortHandler)
		}
	})
}

// previewRoute does no cleaning, redirects or unescaping. Query values are ignored;
// they cannot select a representation. Asset validates UUIDv4 before provider I/O.
func previewRoute(r *http.Request) (string, bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return "", false
	}
	if r.URL.RawPath != "" || r.URL.IsAbs() || r.URL.Opaque != "" {
		return "", false
	}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) != 4 || parts[0] != "" || parts[1] != "media" || parts[3] != "preview" {
		return "", false
	}
	return parts[2], true
}

// deliveryError keeps routine denials quiet and logs only fixed exceptional classes.
func deliveryError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	if errors.Is(err, immich.ErrInvalidID) || errors.Is(err, immich.ErrMissing) || errors.Is(err, immich.ErrUnsupported) {
		publicError(w, r, http.StatusNotFound)
		return
	}
	if r.Context().Err() == nil {
		logger.Warn("provider request failed", "outcome", err)
	}
	publicError(w, r, http.StatusBadGateway)
}

// publicError deliberately constructs identical non-enumerating errors, including
// HEAD headers; neither request text nor provider diagnostics enter the response.
func publicError(w http.ResponseWriter, r *http.Request, status int) {
	body := "not found\n"
	if status == http.StatusBadGateway {
		body = "media unavailable\n"
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, body)
	}
}
