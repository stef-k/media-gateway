package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
	"github.com/stef-k/media-gateway/internal/publication"
)

// Consumer bounds limit one candidate page, raw query parsing and encoded output.
const (
	defaultConsumerLimit  = 25
	maxConsumerQueryBytes = 4096
	maxConsumerJSONBytes  = 64 << 10
)

// consumerAsset is the entire JSON allowlist; never embed private Candidate/Metadata.
type consumerAsset struct {
	ID            string `json:"id"`
	Width         *int64 `json:"width"`
	Height        *int64 `json:"height"`
	FileCreatedAt string `json:"file_created_at"`
	LocalDateTime string `json:"local_date_time"`
	PreviewPath   string `json:"preview_path"`
	// Outer nil omits disabled fields; inner nil emits enabled unknowns as null.
	Latitude  **float64 `json:"latitude,omitempty"`
	Longitude **float64 `json:"longitude,omitempty"`
}

// consumerPage may be empty with continuation because eligibility follows search.
type consumerPage struct {
	Assets     []consumerAsset `json:"assets"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

// gatewayHandler dispatches without path cleaning or redirects. The public handler
// remains independent; nginx must never proxy /internal/, even from loopback.
func gatewayHandler(client *immich.Client, policy config.Policy, settings config.Consumer, logger *slog.Logger) http.Handler {
	public := deliveryHandler(client, policy, logger)
	consumer := consumerHandler(client, policy, settings, logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			consumer.ServeHTTP(w, r)
			return
		}
		public.ServeHTTP(w, r)
	})
}

// consumerHandler authorizes unchanged policy facts before projecting safe fields.
// A loopback peer is required; forwarded headers never establish local identity.
func consumerHandler(client *immich.Client, policy config.Policy, settings config.Consumer, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		query, ok := consumerQuery(r)
		if !ok {
			publicError(w, r, http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), deliveryTimeout)
		defer cancel()
		query.ExposeCoordinates = settings.ExposeCoordinates
		page, err := client.SearchCandidates(ctx, query)
		if err != nil {
			consumerSearchError(w, r, logger, err)
			return
		}
		result := consumerPage{Assets: make([]consumerAsset, 0, len(page.Items)), NextCursor: page.NextCursor}
		for _, item := range page.Items {
			if item.Media != "image" || !publication.Eligible(policy, item.OriginalPath, item.Media) {
				continue
			}
			asset := consumerAsset{
				ID: item.ID, Width: item.Width, Height: item.Height,
				FileCreatedAt: item.FileCreatedAt, LocalDateTime: item.LocalDateTime,
				PreviewPath: "/media/" + item.ID + "/preview",
			}
			if settings.ExposeCoordinates {
				asset.Latitude, asset.Longitude = &item.Latitude, &item.Longitude
			}
			result.Assets = append(result.Assets, asset)
		}
		if query.ID != "" {
			if len(result.Assets) != 1 {
				publicError(w, r, http.StatusNotFound)
				return
			}
			consumerJSON(w, r, result.Assets[0])
			return
		}
		consumerJSON(w, r, result)
	})
}

// consumerQuery allows only pagination on browse and no query on exact detail.
// SearchCandidates validates UUIDv4 and opaque cursor bytes before provider I/O.
func consumerQuery(r *http.Request) (immich.CandidateQuery, bool) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || !net.ParseIP(host).IsLoopback() || r.Method != http.MethodGet || r.URL.RawPath != "" || r.URL.IsAbs() || r.URL.Opaque != "" || len(r.URL.RawQuery) > maxConsumerQueryBytes {
		return immich.CandidateQuery{}, false
	}
	if r.URL.Path != "/internal/assets" {
		id, found := strings.CutPrefix(r.URL.Path, "/internal/assets/")
		return immich.CandidateQuery{ID: id, Limit: 1}, found && id != "" && !strings.Contains(id, "/") && r.URL.RawQuery == "" && !r.URL.ForceQuery
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return immich.CandidateQuery{}, false
	}
	query := immich.CandidateQuery{Limit: defaultConsumerLimit}
	for key, value := range values {
		if len(value) != 1 || value[0] == "" {
			return immich.CandidateQuery{}, false
		}
		switch key {
		case "limit":
			// Only decimal digits; no signs, whitespace or alternate numeric syntax.
			if strings.IndexFunc(value[0], func(c rune) bool { return c < '0' || c > '9' }) >= 0 {
				return immich.CandidateQuery{}, false
			}
			query.Limit, err = strconv.Atoi(value[0])
			if err != nil || query.Limit < 1 || query.Limit > immich.MaxCandidates {
				return immich.CandidateQuery{}, false
			}
		case "cursor":
			query.Cursor = value[0]
		default:
			return immich.CandidateQuery{}, false
		}
	}
	return query, true
}

// consumerJSON commits no success headers until the entire narrow payload fits.
func consumerJSON(w http.ResponseWriter, r *http.Request, value any) {
	body, err := json.Marshal(value)
	if err != nil || len(body) > maxConsumerJSONBytes {
		publicError(w, r, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// consumerSearchError distinguishes invalid/unsupported candidates from failure of
// the search endpoint itself. A missing exact candidate is a successful empty page.
func consumerSearchError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	if errors.Is(err, immich.ErrSearchQuery) || errors.Is(err, immich.ErrInvalidID) || errors.Is(err, immich.ErrUnsupported) {
		publicError(w, r, http.StatusNotFound)
		return
	}
	if errors.Is(err, immich.ErrMissing) {
		// HTTP 404 here means the fixed provider search endpoint failed, not an asset denial.
		err = immich.ErrProvider
	}
	if r.Context().Err() == nil {
		logger.Warn("provider search failed", "outcome", err)
	}
	publicError(w, r, http.StatusBadGateway)
}
