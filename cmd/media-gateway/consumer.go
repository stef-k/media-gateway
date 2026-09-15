package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
	"github.com/stef-k/media-gateway/internal/immich"
	"github.com/stef-k/media-gateway/internal/publication"
)

// Catalogue work and output stay finite even for sparse or changing libraries.
const (
	defaultConsumerLimit  = 25
	maxConsumerQueryBytes = 8 << 10
	maxConsumerJSONBytes  = 512 << 10
	maxCatalogueCalls     = 8
	catalogueTimeout      = 30 * time.Second
)

// consumerAsset is the complete safe allowlist, never embedded provider metadata.
type consumerAsset struct {
	ID             string   `json:"id"`
	MediaType      string   `json:"media_type"`
	Root           string   `json:"root"`
	CollectionPath string   `json:"collection_path"`
	Filename       string   `json:"filename"`
	Width          *int64   `json:"width"`
	Height         *int64   `json:"height"`
	DurationMS     *int64   `json:"duration_ms"`
	FileCreatedAt  string   `json:"file_created_at"`
	LocalDateTime  string   `json:"local_date_time"`
	Latitude       *float64 `json:"latitude"`
	Longitude      *float64 `json:"longitude"`
	PreviewPath    *string  `json:"preview_path"`
	OriginalPath   *string  `json:"original_path"`
}

// consumerPage always carries a nullable gateway continuation.
type consumerPage struct {
	Assets     []consumerAsset `json:"assets"`
	NextCursor *string         `json:"next_cursor"`
}

// consumerCollection is the policy-derived identity; counts are intentionally absent.
type consumerCollection struct {
	Root           string `json:"root"`
	CollectionPath string `json:"collection_path"`
}

// collectionPage makes terminal continuation explicit and carries no counts.
type collectionPage struct {
	Collections []consumerCollection `json:"collections"`
	NextCursor  *string              `json:"next_cursor"`
}

// gatewayHandler dispatches without path cleaning or redirects. nginx must never
// proxy /internal/, even though a same-host proxy appears to have a local peer.
func gatewayHandler(client *immich.Client, policy config.Policy, key cursorKey, logger *slog.Logger) http.Handler {
	public := deliveryHandler(client, policy, logger)
	consumer := consumerHandler(client, policy, key, logger)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/internal/") {
			consumer.ServeHTTP(w, r)
			return
		}
		public.ServeHTTP(w, r)
	})
}

// consumerHandler validates selectors and signatures before bounded provider work.
func consumerHandler(client *immich.Client, policy config.Policy, key cursorKey, logger *slog.Logger) http.Handler {
	streams := immich.DiscoveryStreams(policy)
	policyFingerprint := fingerprint(streams)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		query, ok := consumerQuery(r, policy)
		if !ok {
			publicError(w, r, http.StatusNotFound)
			return
		}
		queryFingerprint := policyFingerprint
		streamCount := len(streams)
		if query.Kind == 'a' {
			queryFingerprint = fingerprint(struct {
				Policy           [32]byte
				Root, Collection string
			}{policyFingerprint, query.Root.Name, query.Collection})
			streamCount = 1
		}
		state, ok := key.verify(query.Cursor, query.Kind, queryFingerprint, streamCount)
		if !ok {
			publicError(w, r, http.StatusNotFound)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), catalogueTimeout)
		defer cancel()
		result, err := catalogue(ctx, client, policy, streams, key, queryFingerprint, query, state)
		if err != nil {
			consumerSearchError(w, r, logger, err)
			return
		}
		consumerJSON(w, r, result)
	})
}

// catalogue fills only remaining output slots; every successful provider page is
// fully consumed. Stream transitions and sparse pages share the eight-call budget.
func catalogue(ctx context.Context, client *immich.Client, policy config.Policy, streams []immich.DiscoveryStream, key cursorKey, hash [32]byte, query catalogueQuery, state continuation) (any, error) {
	assets := consumerPage{Assets: []consumerAsset{}}
	collections := collectionPage{Collections: []consumerCollection{}}
	seen := map[consumerCollection]bool{}
	count, more := 0, true
	for calls := 0; calls < maxCatalogueCalls && count < query.Limit && more; calls++ {
		providerQuery := immich.CandidateQuery{Limit: query.Limit - count, Cursor: state.Provider, ID: query.ID}
		if query.Kind == 'c' {
			if state.Stream >= len(streams) {
				more = false
				break
			}
			providerQuery.Discovery = &streams[state.Stream]
		} else if query.ID == "" {
			providerQuery.Collection = &immich.CollectionSelector{Root: query.Root, Path: query.Collection}
		}
		page, err := client.SearchCandidates(ctx, providerQuery)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			match, eligible := publication.Evaluate(policy, item.OriginalPath, item.Media)
			if !eligible {
				continue
			}
			if query.Kind == 'c' {
				identity := consumerCollection{match.RootName, match.CollectionPath}
				if !seen[identity] {
					seen[identity] = true
					collections.Collections = append(collections.Collections, identity)
					count++
				}
			} else if query.ID != "" || (match.RootName == query.Root.Name && match.CollectionPath == query.Collection) {
				assets.Assets = append(assets.Assets, projectAsset(item, match))
				count++
			}
		}
		state.Provider = page.NextCursor
		more = state.Provider != ""
		if query.Kind == 'c' && !more {
			state.Stream++
			more = state.Stream < len(streams)
		}
		if query.ID != "" {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if query.ID != "" {
		if len(assets.Assets) != 1 {
			return nil, immich.ErrUnsupported
		}
		return assets.Assets[0], nil
	}
	var next *string
	if more {
		next = key.sign(query.Kind, hash, state)
	}
	if query.Kind == 'c' {
		collections.NextCursor = next
		return collections, nil
	}
	assets.NextCursor = next
	return assets, nil
}

// projectAsset advertises only implemented representations after exact authorization.
func projectAsset(item immich.Candidate, match publication.Match) consumerAsset {
	asset := consumerAsset{ID: item.ID, MediaType: item.Media, Root: match.RootName, CollectionPath: match.CollectionPath,
		Filename: path.Base(item.OriginalPath), Width: item.Width, Height: item.Height, DurationMS: item.DurationMS,
		FileCreatedAt: item.FileCreatedAt, LocalDateTime: item.LocalDateTime, Latitude: item.Latitude, Longitude: item.Longitude}
	if item.Media == "image" || item.Media == "video" {
		preview := "/media/" + item.ID + "/preview"
		asset.PreviewPath = &preview
		original := "/media/" + item.ID + "/original"
		asset.OriginalPath = &original
	}
	return asset
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
