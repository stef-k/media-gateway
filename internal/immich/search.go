package immich

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Gateway bounds are deliberately smaller than Immich's 1000-item page maximum.
const (
	MaxCandidates           = 100
	MaxCandidateCursorBytes = 1024
)

// ErrSearchQuery is a fixed, log-safe input validation outcome.
var ErrSearchQuery = errors.New("immich: invalid candidate query")

// CandidateQuery exposes no provider URL, path, filter tree or ordering controls.
// Limit must be 1..MaxCandidates. An optional UUIDv4 ID selects one exact image;
// ID and Cursor cannot be combined. Empty Cursor starts a new search.
type CandidateQuery struct {
	Limit  int
	Cursor string
	ID     string
	// ExposeCoordinates is set only from trusted consumer configuration.
	ExposeCoordinates bool
}

// Candidate is untrusted for publication. OriginalPath is unchanged private
// metadata for publication.Evaluate, never a consumer field or a local file to open.
// Nil dimensions mean unknown; time strings retain provider precision/local meaning.
// FileCreatedAt is capture time; LocalDateTime is timezone-agnostic local wall time.
type Candidate struct {
	Metadata
	Width         *int64
	Height        *int64
	FileCreatedAt string
	LocalDateTime string
	// Coordinates are a validated nullable pair, never publication authority.
	Latitude  *float64
	Longitude *float64
}

// CandidatePage contains at most the requested limit; empty NextCursor means done.
// Callers must authorize each item before exposing anything to a consumer, and
// must never log the page or its opaque provider cursor.
type CandidatePage struct {
	Items      []Candidate
	NextCursor string
}

// SearchCandidates makes one bounded request, with no retries or automatic paging.
// Search narrows to images, but grants no publication authority. Every failure
// returns a zero page and a sanitized sentinel; no per-request logs are emitted.
func (c *Client) SearchCandidates(ctx context.Context, query CandidateQuery) (CandidatePage, error) {
	if query.ID != "" && !uuidV4.MatchString(query.ID) {
		return CandidatePage{}, ErrInvalidID
	}
	if query.Limit < 1 || query.Limit > MaxCandidates || !validCandidateCursor(query.Cursor) || (query.ID != "" && query.Cursor != "") {
		return CandidatePage{}, ErrSearchQuery
	}
	// Only gateway-owned keys/operators can appear in the structured search body.
	filter := map[string]any{"type": map[string]string{"eq": "IMAGE"}}
	if query.ID != "" {
		filter["id"] = map[string]string{"eq": query.ID}
	}
	payload := map[string]any{
		"filter": filter, "orderBy": map[string]string{"field": "fileCreatedAt", "direction": "desc"},
		"size": query.Limit, "withExif": query.ExposeCoordinates, "withPeople": false, "withStacked": false,
	}
	if query.Cursor != "" {
		payload["cursor"] = query.Cursor
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return CandidatePage{}, ErrSearchQuery
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.searchEndpoint, bytes.NewReader(body))
	if err != nil {
		return CandidatePage{}, ErrTransport
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return CandidatePage{}, transportError(ctx, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return CandidatePage{}, ErrMissing
	case http.StatusUnauthorized, http.StatusForbidden:
		return CandidatePage{}, ErrAuth
	default:
		return CandidatePage{}, ErrProvider
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" || len(resp.Header.Values("Content-Type")) != 1 || resp.Header.Get("Content-Range") != "" {
		return CandidatePage{}, ErrMetadata
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxMetadata+1))
	if err != nil {
		return CandidatePage{}, transportError(ctx, err)
	}
	if len(body) > maxMetadata || !utf8.Valid(body) {
		return CandidatePage{}, ErrMetadata
	}
	return decodeCandidatePage(body, query)
}

// validCandidateCursor preserves opaque text while bounding size and controls.
func validCandidateCursor(cursor string) bool {
	return len(cursor) <= MaxCandidateCursorBytes && utf8.ValidString(cursor) && strings.IndexFunc(cursor, unicode.IsControl) < 0
}

// decodeCandidatePage requires the consumed envelope fields, including explicit
// null for a terminal cursor. Unrelated provider response fields are ignored.
func decodeCandidatePage(body []byte, query CandidateQuery) (CandidatePage, error) {
	var wire struct {
		Assets *struct {
			Items      []json.RawMessage `json:"items"`
			Count      *int              `json:"count"`
			NextCursor json.RawMessage   `json:"nextCursor"`
		} `json:"assets"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Assets == nil {
		return CandidatePage{}, ErrMetadata
	}
	assets := wire.Assets
	if assets.Items == nil || assets.Count == nil || *assets.Count != len(assets.Items) || len(assets.Items) > query.Limit || (query.ID != "" && len(assets.Items) > 1) {
		return CandidatePage{}, ErrMetadata
	}
	var cursor string
	if json.Unmarshal(assets.NextCursor, &cursor) != nil || !validCandidateCursor(cursor) {
		return CandidatePage{}, ErrMetadata
	}
	if cursor == "" && !bytes.Equal(bytes.TrimSpace(assets.NextCursor), []byte("null")) {
		return CandidatePage{}, ErrMetadata
	}
	page := CandidatePage{Items: make([]Candidate, 0, len(assets.Items)), NextCursor: cursor}
	for _, raw := range assets.Items {
		item, err := decodeCandidate(raw, query.ID, query.ExposeCoordinates)
		// Structured Immich search can return lifecycle-unavailable records.
		// Omit them without losing the provider cursor or failing active siblings.
		if errors.Is(err, ErrMissing) {
			continue
		}
		if err != nil {
			return CandidatePage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, nil
}

// decodeCandidate retains policy facts, dimensions/times and opt-in coordinates.
func decodeCandidate(raw []byte, requested string, exposeCoordinates bool) (Candidate, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return Candidate{}, ErrMetadata
	}
	var id string
	if json.Unmarshal(fields["id"], &id) != nil || !uuidV4.MatchString(id) || (requested != "" && !strings.EqualFold(id, requested)) {
		return Candidate{}, ErrMetadata
	}
	metadata, err := decodeMetadata(raw, id)
	if err != nil {
		return Candidate{}, err
	}
	if metadata.Media != "image" {
		return Candidate{}, ErrUnsupported
	}
	item := Candidate{Metadata: metadata}
	for name, target := range map[string]**int64{"width": &item.Width, "height": &item.Height} {
		if json.Unmarshal(fields[name], target) != nil {
			return Candidate{}, ErrMetadata
		}
		if *target != nil && (**target < 0 || **target > 9007199254740991) {
			return Candidate{}, ErrMetadata
		}
	}
	for name, target := range map[string]*string{"fileCreatedAt": &item.FileCreatedAt, "localDateTime": &item.LocalDateTime} {
		if json.Unmarshal(fields[name], target) != nil {
			return Candidate{}, ErrMetadata
		}
		if _, err := time.Parse(time.RFC3339Nano, *target); err != nil {
			return Candidate{}, ErrMetadata
		}
	}
	if exposeCoordinates {
		item.Latitude, item.Longitude, err = decodeCoordinates(fields["exifInfo"])
		if err != nil {
			return Candidate{}, err
		}
	}
	return item, nil
}
