// Package immich retrieves private metadata and previews; it never grants publication.
package immich

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/stef-k/media-gateway/internal/config"
)

// Stable outcome classes contain no upstream values and are safe to log.
var (
	ErrInvalidID   = errors.New("immich: invalid asset identifier")
	ErrMissing     = errors.New("immich: asset missing")
	ErrAuth        = errors.New("immich: authentication or permission failure")
	ErrProvider    = errors.New("immich: unexpected provider status")
	ErrTransport   = errors.New("immich: transport failure")
	ErrMetadata    = errors.New("immich: invalid metadata response")
	ErrUnsupported = errors.New("immich: unsupported media type")
)

// Metadata contains only policy inputs and identity. Never log it or expose it publicly.
// OriginalPath is unchanged provider metadata, not a local file to open.
type Metadata struct {
	ID           string
	OriginalPath string
	Media        string
}

// Client owns a fixed provider authority and credential. Reuse it across requests.
// Construct it with New; it is safe for concurrent use and emits no logs.
type Client struct {
	endpoint       string
	searchEndpoint string
	key            string
	http           *http.Client
}

// New consumes the provider configuration and separate key returned by config.Load.
// Those validated inputs must remain trusted; this function performs no file I/O.
func New(provider config.Provider, key string) *Client {
	return &Client{
		endpoint:       strings.TrimSuffix(provider.BaseURL, "/") + "/api/assets/",
		searchEndpoint: strings.TrimSuffix(provider.BaseURL, "/") + "/api/search/metadata",
		key:            key,
		http: &http.Client{
			Timeout:       provider.RequestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
			Transport: &http.Transport{
				// No environment proxy may receive the private provider credential.
				DialContext:            (&net.Dialer{Timeout: min(5*time.Second, provider.RequestTimeout)}).DialContext,
				TLSHandshakeTimeout:    min(5*time.Second, provider.RequestTimeout),
				ResponseHeaderTimeout:  provider.RequestTimeout,
				MaxResponseHeaderBytes: 16 << 10,
				IdleConnTimeout:        30 * time.Second,
			},
		},
	}
}

// CloseIdleConnections releases idle pooled connections without interrupting requests.
func (c *Client) CloseIdleConnections() { c.http.CloseIdleConnections() }

// uuidV4 follows the reviewed Immich OpenAPI pattern, including version and variant.
var uuidV4 = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-4[0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

// maxMetadata bounds the entire JSON document, including ignored provider fields.
const maxMetadata = 1 << 20

// Asset retrieves currently available metadata with no cache or policy decision. Every error
// returns zero metadata. Context errors are safe sentinels, never wrapped URL errors.
func (c *Client) Asset(ctx context.Context, id string) (Metadata, error) {
	if !uuidV4.MatchString(id) {
		return Metadata{}, ErrInvalidID
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+id, nil)
	if err != nil {
		return Metadata{}, ErrTransport
	}
	req.Header.Set("x-api-key", c.key)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return Metadata{}, transportError(ctx, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	// Immich v3.2.0 uses 400 for missing or inaccessible UUID-prevalidated assets.
	case http.StatusBadRequest, http.StatusNotFound:
		return Metadata{}, ErrMissing
	case http.StatusUnauthorized, http.StatusForbidden:
		return Metadata{}, ErrAuth
	default:
		return Metadata{}, ErrProvider
	}
	contentType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return Metadata{}, ErrMetadata
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadata+1))
	if err != nil {
		return Metadata{}, transportError(ctx, err)
	}
	if len(body) > maxMetadata {
		return Metadata{}, ErrMetadata
	}
	return decodeMetadata(body, id)
}

// transportError deliberately discards provider, URL and caller-supplied cause text.
func transportError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return context.DeadlineExceeded
	}
	return ErrTransport
}

// decodeMetadata requires current lifecycle availability before returning policy inputs.
// Lifecycle flags stay private to decoding; missing/null flags must not default to active.
func decodeMetadata(body []byte, requested string) (Metadata, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(body, &fields) != nil {
		return Metadata{}, ErrMetadata
	}
	var isTrashed, isOffline *bool
	if json.Unmarshal(fields["isTrashed"], &isTrashed) != nil || isTrashed == nil ||
		json.Unmarshal(fields["isOffline"], &isOffline) != nil || isOffline == nil {
		return Metadata{}, ErrMetadata
	}
	if *isTrashed || *isOffline {
		return Metadata{}, ErrMissing
	}
	var id, originalPath, media string
	for name, target := range map[string]*string{"id": &id, "originalPath": &originalPath, "type": &media} {
		if json.Unmarshal(fields[name], target) != nil || *target == "" {
			return Metadata{}, ErrMetadata
		}
	}
	if !strings.EqualFold(id, requested) {
		return Metadata{}, ErrMetadata
	}
	switch media {
	case "IMAGE":
		media = "image"
	case "VIDEO":
		media = "video"
	default:
		return Metadata{}, ErrUnsupported
	}
	return Metadata{ID: id, OriginalPath: originalPath, Media: media}, nil
}

// ValidAssetID recognizes the reviewed UUIDv4 route identity before provider I/O.
func ValidAssetID(id string) bool { return uuidV4.MatchString(id) }
